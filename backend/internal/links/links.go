// Package links rendert die in der config.yaml konfigurierten Links (z. B. Absprung nach Kibana) je Objekt.
package links

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// Link ist ein fertig gerenderter Link für das Frontend.
type Link struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	LucideIcon      string `json:"lucideIcon"`
	LucideIconColor string `json:"lucideIconColor"`
	URL             string `json:"url"`
}

type compiled struct {
	cfg         config.Link
	url         *template.Template
	targets     map[model.Kind]map[string]*template.Template
	queryParams map[string]*template.Template
	queryKeys   []string
}

// Registry hält die beim Start geparsten Link-Templates.
type Registry struct {
	links       []compiled
	globalVars  map[string]string
	projectVars map[string]map[string]string
}

// lineBreaks erlaubt, lange URL-Templates in der config.yaml umzubrechen.
var lineBreaks = regexp.MustCompile(`[ \t]*\r?\n[ \t]*`)

// NewRegistry parst alle Link-Templates. Fehler brechen den Start des Backends ab.
func NewRegistry(cfg *config.Config) (*Registry, error) {
	r := &Registry{globalVars: cfg.LinkVariables, projectVars: map[string]map[string]string{}}
	for _, p := range cfg.Projects {
		r.projectVars[p.Name] = p.LinkVariables
	}
	var errs []error
	for _, l := range cfg.Links {
		c, err := compile(l)
		if err != nil {
			errs = append(errs, fmt.Errorf("link %q: %w", l.ID, err))
			continue
		}
		r.links = append(r.links, c)
	}
	return r, errors.Join(errs...)
}

func compile(l config.Link) (compiled, error) {
	var errs []error
	c := compiled{cfg: l, targets: map[model.Kind]map[string]*template.Template{}, queryParams: map[string]*template.Template{}}
	var err error
	if c.url, err = objecttemplate.Parse("url", lineBreaks.ReplaceAllString(l.URL, "")); err != nil {
		errs = append(errs, err)
	}
	for kind, values := range l.Targets {
		c.targets[kind] = map[string]*template.Template{}
		for key, text := range values {
			t, err := objecttemplate.Parse(fmt.Sprintf("targets.%s.%s", kind, key), text)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			c.targets[kind][key] = t
		}
	}
	for key, text := range l.QueryParams {
		t, err := objecttemplate.Parse("query-params."+key, text)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.queryParams[key] = t
	}
	c.queryKeys = slices.Sorted(maps.Keys(c.queryParams))
	return c, errors.Join(errs...)
}

// For liefert die Links zu einem Objekt (nie nil). obj ist ein model.Stored*-Wert, project sein Projekt.
// Links, deren Templates für dieses Objekt nicht gerendert werden können, werden weggelassen und geloggt.
func (r *Registry) For(ref model.ObjectRef, obj any, project string) []Link {
	result := []Link{}
	if r == nil {
		return result
	}
	for _, c := range r.links {
		targetTemplates, ok := c.targets[ref.Kind]
		if !ok || (len(c.cfg.Projects) > 0 && !slices.Contains(c.cfg.Projects, project)) {
			continue
		}
		u, err := r.render(c, targetTemplates, ref, obj, project)
		if err != nil {
			slog.Warn("Link konnte nicht erzeugt werden", "link", c.cfg.ID, "object", ref, "error", err)
			continue
		}
		result = append(result, Link{
			ID:              c.cfg.ID,
			Title:           c.cfg.Title,
			LucideIcon:      c.cfg.LucideIcon,
			LucideIconColor: c.cfg.LucideIconColor,
			URL:             u,
		})
	}
	return result
}

func (r *Registry) render(c compiled, targetTemplates map[string]*template.Template, ref model.ObjectRef, obj any, project string) (string, error) {
	data, err := objecttemplate.NewData(ref, obj, project)
	if err != nil {
		return "", err
	}
	maps.Copy(data.Vars, r.globalVars)
	maps.Copy(data.Vars, r.projectVars[project])

	for _, k := range slices.Sorted(maps.Keys(targetTemplates)) {
		if data.Target[k], err = objecttemplate.Render(targetTemplates[k], data); err != nil {
			return "", err
		}
	}

	raw, err := objecttemplate.Render(c.url, data)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ungültige URL: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("nur absolute http(s)-URLs sind erlaubt, erhalten: %q", raw)
	}
	if len(c.queryKeys) > 0 {
		if err := c.appendQueryParams(u, data); err != nil {
			return "", err
		}
	}
	return u.String(), nil
}

// appendQueryParams rendert die query-params und hängt sie an u an. Mit encode-query-params: false werden Namen
// und Werte unverändert angehängt, z. B. für Ziele, die bereits kodierte oder eigene Notationen erwarten.
func (c compiled) appendQueryParams(u *url.URL, data objecttemplate.Data) error {
	values := make([]string, len(c.queryKeys))
	for i, k := range c.queryKeys {
		v, err := objecttemplate.Render(c.queryParams[k], data)
		if err != nil {
			return err
		}
		values[i] = v
	}

	if c.cfg.EncodesQueryParams() {
		q := u.Query()
		for i, k := range c.queryKeys {
			q.Set(k, values[i])
		}
		u.RawQuery = q.Encode()
		return nil
	}

	pairs := make([]string, 0, len(c.queryKeys)+1)
	if u.RawQuery != "" {
		pairs = append(pairs, u.RawQuery)
	}
	for i, k := range c.queryKeys {
		pair := k + "=" + values[i]
		// Diese Zeichen würden die URL zerstören (Fragment-Beginn bzw. ungültige URL).
		if strings.ContainsAny(pair, "# \t\r\n") {
			return fmt.Errorf("query-param %q enthält ohne URL-Kodierung unzulässige Zeichen (#, Leerzeichen, Zeilenumbruch)", k)
		}
		pairs = append(pairs, pair)
	}
	u.RawQuery = strings.Join(pairs, "&")
	return nil
}
