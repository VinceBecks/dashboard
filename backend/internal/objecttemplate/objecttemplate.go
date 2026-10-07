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
	// Vars setzt der Aufrufer (z. B. link-variables bei Links).
	Vars map[string]string
	// Services setzt der Aufrufer (z. B. die gewählten Services bei deploy-missing-services), sonst leer.
	Services []string
	// Container setzt der Aufrufer bei Kopier-Befehlen für Pods (je Container ein Befehl), sonst leer.
	Container string
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
		Services:    []string{},
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
	"shellquote": ShellQuote,
	// trimproject ist hier nur ein Platzhalter für das Parsen; Render bindet es an das Projekt des Objekts.
	"trimproject": func(s string) string { return s },
}

// TrimProject entfernt den Projektnamen samt Bindestrich als Präfix, z. B. "myproject-foo-bar" im Projekt
// "myproject" zu "foo-bar". Ohne dieses Präfix (oder ohne Projekt) bleibt s unverändert.
func TrimProject(s, project string) string {
	if project == "" {
		return s
	}
	return strings.TrimPrefix(s, project+"-")
}

// ShellQuote setzt s für POSIX-Shells in einfache Anführungszeichen; enthaltene Hochkommas werden maskiert,
// sodass der Wert als genau ein Argument ankommt.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Parse parst ein Template. Fehlende Map-Schlüssel (z. B. .Vars.unbekannt) führen beim Ausführen zu einem Fehler.
func Parse(name, text string) (*template.Template, error) {
	t, err := template.New(name).Option("missingkey=error").Funcs(Funcs).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return t, nil
}

// Render führt ein Template aus. Funktionen, die vom Objekt abhängen (trimproject), werden dafür an eine Kopie des
// Templates gebunden; das geparste Template selbst bleibt unverändert und kann nebenläufig genutzt werden.
func Render(t *template.Template, data Data) (string, error) {
	bound, err := t.Clone()
	if err != nil {
		return "", fmt.Errorf("template %s: %w", t.Name(), err)
	}
	bound.Funcs(template.FuncMap{"trimproject": func(s string) string { return TrimProject(s, data.Project) }})
	var sb strings.Builder
	if err := bound.Execute(&sb, data); err != nil {
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
