// Package gitlabpipeline implementiert den Handler-Typ „gitlab-pipeline“: Er startet über die Pipeline-Trigger-API
// eine GitLab-Pipeline. Beliebig viele Aktionen können diesen Typ nutzen und unterscheiden sich nur über ihre
// Konfiguration (Projekt, ref, Variablen, Anwendbarkeit).
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
	"slices"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// HandlerType ist der Wert für `handler:` in der config.yaml.
const HandlerType = "gitlab-pipeline"

const (
	defaultTimeout  = 10 * time.Second
	maxResponseSize = 1 << 20
	maxMessageLen   = 300
)

// settings sind die extra-handler-configs einer Aktion.
type settings struct {
	GitLabURL string       `yaml:"gitlab-url"`
	Project   string       `yaml:"project"`
	Ref       string       `yaml:"ref"`
	TokenEnv  string       `yaml:"token-env"`
	AppliesTo []model.Kind `yaml:"applies-to"`
	// Projects gibt es nur noch als allgemeines Feld der Aktion; hier nur, um alte Configs zu erkennen.
	Projects  []string          `yaml:"projects"`
	Timeout   string            `yaml:"timeout"`
	Variables map[string]string `yaml:"variables"`
}

// Handler startet eine GitLab-Pipeline.
type Handler struct {
	client     *http.Client
	triggerURL string
	token      string
	appliesTo  []model.Kind
	ref        *template.Template
	variables  map[string]*template.Template
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{}
}

// Configure liest und prüft die extra-handler-configs. Fehler brechen den Start des Backends ab.
func (h *Handler) Configure(extra map[string]any) error {
	var s settings
	raw, err := yaml.Marshal(extra)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("extra-handler-configs: %w", err)
	}

	var errs []error
	base, err := url.Parse(s.GitLabURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		errs = append(errs, fmt.Errorf("gitlab-url muss eine absolute http(s)-URL sein"))
	} else {
		h.triggerURL = strings.TrimSuffix(base.String(), "/") + "/api/v4/projects/" + url.PathEscape(s.Project) + "/trigger/pipeline"
	}
	if s.Project == "" {
		errs = append(errs, errors.New("project fehlt"))
	}
	if s.TokenEnv == "" {
		errs = append(errs, errors.New("token-env fehlt"))
	} else if h.token = os.Getenv(s.TokenEnv); h.token == "" {
		errs = append(errs, fmt.Errorf("umgebungsvariable %s mit dem Trigger-Token ist nicht gesetzt", s.TokenEnv))
	}
	if len(s.AppliesTo) == 0 {
		errs = append(errs, errors.New("applies-to fehlt"))
	}
	for _, k := range s.AppliesTo {
		if !k.Valid() {
			errs = append(errs, fmt.Errorf("applies-to: ungültiger Objekt-Typ %q", k))
		}
	}
	h.appliesTo = s.AppliesTo
	if s.Projects != nil {
		errs = append(errs, errors.New("projects gehört nicht mehr in extra-handler-configs, sondern direkt an die Aktion (projects: [...])"))
	}

	timeout := defaultTimeout
	if s.Timeout != "" {
		if timeout, err = time.ParseDuration(s.Timeout); err != nil || timeout <= 0 {
			errs = append(errs, fmt.Errorf("timeout %q ist ungültig", s.Timeout))
		}
	}
	h.client = &http.Client{Timeout: timeout}

	if s.Ref == "" {
		errs = append(errs, errors.New("ref fehlt"))
	} else if h.ref, err = objecttemplate.Parse("ref", s.Ref); err != nil {
		errs = append(errs, err)
	}
	h.variables = make(map[string]*template.Template, len(s.Variables))
	for key, value := range s.Variables {
		t, err := objecttemplate.Parse("variables."+key, value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		h.variables[key] = t
	}
	return errors.Join(errs...)
}

// IsApplicable prüft den Objekt-Typ (applies-to). Die Projekte prüft bereits die Registry (projects der Aktion).
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	return slices.Contains(h.appliesTo, obj.Ref.Kind)
}

// FormFields: keine Eingaben; das Frontend fragt nur die Bestätigung ab.
func (h *Handler) FormFields(actions.StoredObject) []actions.FormField { return nil }

// Execute rendert ref und Variablen und löst die Pipeline aus.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, _ map[string]any) actions.Result {
	data, err := h.templateData(obj)
	if err != nil {
		return failure("Objekt-Daten nicht lesbar: %v", err)
	}
	form := url.Values{}
	form.Set("token", h.token)
	ref, err := objecttemplate.Render(h.ref, data)
	if err != nil {
		return failure("%v", err)
	}
	form.Set("ref", ref)
	for key, t := range h.variables {
		value, err := objecttemplate.Render(t, data)
		if err != nil {
			return failure("%v", err)
		}
		form.Set("variables["+key+"]", value)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.triggerURL, strings.NewReader(form.Encode()))
	if err != nil {
		return failure("Request konnte nicht erstellt werden: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := h.client.Do(req)
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

func (h *Handler) templateData(obj actions.StoredObject) (objecttemplate.Data, error) {
	return objecttemplate.NewData(obj.Ref, obj.Object, obj.Project)
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
