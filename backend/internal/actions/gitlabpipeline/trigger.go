package gitlabpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/objecttemplate"
)

const (
	defaultTimeout  = 10 * time.Second
	maxResponseSize = 1 << 20
	maxMessageLen   = 300
)

// TriggerConfig sind die GitLab-Angaben in den extra-handler-configs. Handler, die eine Pipeline starten, betten
// sie per `yaml:",inline"` in ihre eigenen Settings ein.
type TriggerConfig struct {
	GitLabURL string            `yaml:"gitlab-url"`
	Project   string            `yaml:"project"`
	Ref       string            `yaml:"ref"`
	TokenEnv  string            `yaml:"token-env"`
	Timeout   string            `yaml:"timeout"`
	Variables map[string]string `yaml:"variables"`
}

// Trigger startet eine GitLab-Pipeline über die Pipeline-Trigger-API.
type Trigger struct {
	client     *http.Client
	triggerURL string
	token      string
	ref        *template.Template
	variables  map[string]*template.Template
}

// NewTrigger prüft c, liest das Token aus der Umgebung und parst die Templates.
func NewTrigger(c TriggerConfig) (*Trigger, error) {
	t := &Trigger{}
	var errs []error
	base, err := url.Parse(c.GitLabURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		errs = append(errs, fmt.Errorf("gitlab-url muss eine absolute http(s)-URL sein"))
	} else {
		t.triggerURL = strings.TrimSuffix(base.String(), "/") + "/api/v4/projects/" + url.PathEscape(c.Project) + "/trigger/pipeline"
	}
	if c.Project == "" {
		errs = append(errs, errors.New("project fehlt"))
	}
	if c.TokenEnv == "" {
		errs = append(errs, errors.New("token-env fehlt"))
	} else if t.token = os.Getenv(c.TokenEnv); t.token == "" {
		errs = append(errs, fmt.Errorf("umgebungsvariable %s mit dem Trigger-Token ist nicht gesetzt", c.TokenEnv))
	}

	timeout := defaultTimeout
	if c.Timeout != "" {
		if timeout, err = time.ParseDuration(c.Timeout); err != nil || timeout <= 0 {
			errs = append(errs, fmt.Errorf("timeout %q ist ungültig", c.Timeout))
		}
	}
	t.client = &http.Client{Timeout: timeout}

	if c.Ref == "" {
		errs = append(errs, errors.New("ref fehlt"))
	} else if t.ref, err = objecttemplate.Parse("ref", c.Ref); err != nil {
		errs = append(errs, err)
	}
	t.variables = make(map[string]*template.Template, len(c.Variables))
	for key, value := range c.Variables {
		tmpl, err := objecttemplate.Parse("variables."+key, value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		t.variables[key] = tmpl
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return t, nil
}

// Run rendert ref und Variablen mit data und löst die Pipeline aus.
func (t *Trigger) Run(ctx context.Context, data objecttemplate.Data) actions.Result {
	form := url.Values{}
	form.Set("token", t.token)
	ref, err := objecttemplate.Render(t.ref, data)
	if err != nil {
		return failure("%v", err)
	}
	form.Set("ref", ref)
	for key, tmpl := range t.variables {
		value, err := objecttemplate.Render(tmpl, data)
		if err != nil {
			return failure("%v", err)
		}
		form.Set("variables["+key+"]", value)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.triggerURL, strings.NewReader(form.Encode()))
	if err != nil {
		return failure("Request konnte nicht erstellt werden: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := t.client.Do(req)
	if err != nil {
		// Der Fehler enthält nur Methode und URL; das Token steht im Body und taucht nicht auf.
		return failure("GitLab nicht erreichbar: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxResponseSize))

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		return failure("GitLab antwortete mit %s: %s", res.Status, gitlabMessage(body))
	}
	var pipeline struct {
		ID     int    `json:"id"`
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(body, &pipeline); err != nil || pipeline.ID == 0 {
		return actions.Result{Success: true, Message: fmt.Sprintf("Pipeline für ref %s gestartet", ref)}
	}
	return actions.Result{Success: true, Message: fmt.Sprintf("Pipeline #%d gestartet: %s", pipeline.ID, pipeline.WebURL)}
}

// gitlabMessage liest das Feld "message" einer GitLab-Fehlerantwort, sonst den gekürzten Body.
func gitlabMessage(body []byte) string {
	var parsed struct {
		Message any `json:"message"`
		Error   any `json:"error"`
	}
	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &parsed) == nil {
		if parsed.Message != nil {
			msg = fmt.Sprint(parsed.Message)
		} else if parsed.Error != nil {
			msg = fmt.Sprint(parsed.Error)
		}
	}
	if len(msg) > maxMessageLen {
		msg = msg[:maxMessageLen] + "…"
	}
	return msg
}

func failure(format string, args ...any) actions.Result {
	return actions.Result{Success: false, Message: fmt.Sprintf(format, args...)}
}
