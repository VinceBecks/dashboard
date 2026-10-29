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
	"text/template"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// Link ist ein fertig gerenderter Link für das Frontend.
type Link struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	icon      string `json:"icon"`
	iconColor string `json:"iconColor"`
	URL       string `json:"url"`
}

type compiled struct {
	cfg  config.Link
	urls map[model.Kind]*template.Template
}

// Registry hält die beim Start geparsten Link-Templates.
type Registry struct {
	links       []compiled
	globalVars  map[string]string
	projectVars map[string]map[string]string
}

// lineBreaks erlaubt, lange URL-Templates in der config.yaml umzubrechen (YAML-Blockstil |-).
var lineBreaks = regexp.MustCompile(`[ \t]*\r?\n[ \t]*`)

// NewRegistry parst alle Link-Templates. Fehler brechen den Start des Backends ab.
func NewRegistry(cfg *config.Config) (*Registry, error) {
	r := &Registry{globalVars: cfg.LinkVariables, projectVars: map[string]map[string]string{}}
	for _, p := range cfg.Projects {
		r.projectVars[p.Name] = p.LinkVariablesOverrides
	}
	var errs []error
	for _, l := range cfg.Links {
		if len(l.Projects) == 0 {
			slog.Warn("Link ist in keinem Projekt verfügbar (projects ist leer)", "link", l.ID)
		}
		c := compiled{cfg: l, urls: map[model.Kind]*template.Template{}}
		for kind, target := range l.Targets {
			t, err := objecttemplate.Parse(fmt.Sprintf("links.%s.targets.%s.url", l.ID, kind), lineBreaks.ReplaceAllString(target.URL, ""))
			if err != nil {
				errs = append(errs, err)
				continue
			}
			c.urls[kind] = t
		}
		r.links = append(r.links, c)
	}
	return r, errors.Join(errs...)
}

// For liefert die Links zu einem Objekt (nie nil). obj ist ein model.Stored*-Wert, project sein Projekt.
// Links, deren URL für dieses Objekt nicht gerendert werden kann, werden weggelassen und geloggt.
func (r *Registry) For(ref model.ObjectRef, obj any, project string) []Link {
	result := []Link{}
	if r == nil {
		return result
	}
	for _, c := range r.links {
		tpl, ok := c.urls[ref.Kind]
		if !ok || !slices.Contains(c.cfg.Projects, project) {
			continue
		}
		u, err := r.render(tpl, ref, obj, project)
		if err != nil {
			slog.Warn("Link konnte nicht erzeugt werden", "link", c.cfg.ID, "object", ref, "error", err)
			continue
		}
		result = append(result, Link{
			ID:        c.cfg.ID,
			Title:     c.cfg.Title,
			icon:      c.cfg.Icon,
			iconColor: c.cfg.IconColor,
			URL:       u,
		})
	}
	return result
}

func (r *Registry) render(tpl *template.Template, ref model.ObjectRef, obj any, project string) (string, error) {
	data, err := objecttemplate.NewData(ref, obj, project)
	if err != nil {
		return "", err
	}
	maps.Copy(data.Vars, r.globalVars)
	maps.Copy(data.Vars, r.projectVars[project])

	raw, err := objecttemplate.Render(tpl, data)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("ungültige URL. Raw String wert ist '%s': %w", raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("nur absolute http(s)-URLs sind erlaubt, erhalten: %q", raw)
	}
	return u.String(), nil
}
