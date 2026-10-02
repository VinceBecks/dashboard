// Package objecttemplate stellt Go-Templates mit den Daten eines Stored-Objects bereit. Es wird von
// konfigurierbaren Funktionen genutzt, z. B. für Pipeline-Variablen (gitlab-pipeline) und Links.
package objecttemplate

import (
	"fmt"
	"maps"
	"net/url"
	"strings"
	"text/template"

	"feature-dashboard/internal/model"
)

// Data sind die Werte, die in Templates zur Verfügung stehen, z. B. "{{ .Namespace }}",
// `{{ index .Labels "app" }}` oder "{{ .Vars.kibanaUrl }}".
type Data struct {
	Kind        model.Kind
	Name        string
	Namespace   string
	Project     string
	Labels      map[string]string
	Annotations map[string]string
	// Selector ist nur bei Services gefüllt.
	Selector map[string]string
	// Vars und Target werden vom Aufrufer gesetzt (z. B. link-variables und targets bei Links).
	Vars   map[string]string
	Target map[string]string
}

// NewData baut die Template-Daten zu einem Objekt. obj ist ein model.Stored*-Wert.
func NewData(ref model.ObjectRef, obj any, project string) (Data, error) {
	meta, ok := Meta(obj)
	if !ok {
		return Data{}, fmt.Errorf("unbekannter Objekt-Typ %T", obj)
	}
	d := Data{
		Kind:        ref.Kind,
		Name:        ref.Name,
		Namespace:   ref.Namespace,
		Project:     project,
		Labels:      nonNil(meta.Labels),
		Annotations: nonNil(meta.Annotations),
		Selector:    map[string]string{},
		Vars:        map[string]string{},
		Target:      map[string]string{},
	}
	if svc, ok := obj.(model.StoredService); ok {
		d.Selector = nonNil(maps.Clone(svc.Selector))
	}
	return d, nil
}

// Meta liefert die gemeinsamen Metadaten eines Stored-Objects.
func Meta(obj any) (model.Meta, bool) {
	switch o := obj.(type) {
	case model.StoredNamespace:
		return o.Meta, true
	case model.StoredService:
		return o.Meta, true
	case model.StoredIngress:
		return o.Meta, true
	case model.StoredPod:
		return o.Meta, true
	}
	return model.Meta{}, false
}

// Funcs sind die zusätzlichen Template-Funktionen (neben Go-Standardfunktionen wie urlquery).
var Funcs = template.FuncMap{
	"pathescape": url.PathEscape,
	"rison":      Rison,
	"replace":    func(s, old, new string) string { return strings.ReplaceAll(s, old, new) },
	"join":       func(elems []string, sep string) string { return strings.Join(elems, sep) },
}

// Parse parst ein Template. Fehlende Map-Schlüssel (z. B. .Vars.unbekannt) führen beim Ausführen zu einem Fehler.
func Parse(name, text string) (*template.Template, error) {
	t, err := template.New(name).Option("missingkey=error").Funcs(Funcs).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return t, nil
}

// Render führt ein Template aus.
func Render(t *template.Template, data Data) (string, error) {
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("template %s: %w", t.Name(), err)
	}
	return sb.String(), nil
}

// Rison kodiert s als Rison-String, wie Kibana ihn in URLs erwartet: in Hochkommas, mit ! als Escape-Zeichen.
func Rison(s string) string {
	return "'" + strings.NewReplacer("!", "!!", "'", "!'").Replace(s) + "'"
}

func nonNil(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
