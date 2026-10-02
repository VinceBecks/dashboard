package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"feature-dashboard/internal/config"
)

var (
	// ErrUnknownAction: Aktion existiert nicht oder ist nicht aktiviert.
	ErrUnknownAction = errors.New("unbekannte oder deaktivierte Aktion")
	// ErrNotApplicable: Aktion ist auf dem Objekt nicht ausführbar.
	ErrNotApplicable = errors.New("aktion ist auf diesem Objekt nicht ausführbar")
)

// ValidationError meldet ungültige Formular-Eingaben.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string { return fmt.Sprintf("ungültige Eingaben: %v", e.Problems) }

// Action ist die Beschreibung einer auf einem Objekt ausführbaren Aktion für das Frontend.
type Action struct {
	ID              string      `json:"id"`
	Title           string      `json:"title"`
	LucideIcon      string      `json:"lucideIcon"`
	LucideIconColor string      `json:"lucideIconColor"`
	FormFields      []FormField `json:"formFields,omitempty"`
}

type activeAction struct {
	cfg     config.ActionHandlerConfig
	handler Handler
}

// Registry kennt alle registrierten Handler-Factories und die aktivierten Aktionen.
type Registry struct {
	factories map[string]Factory
	active    []activeAction
}

// NewRegistry erzeugt eine leere Registry.
func NewRegistry() *Registry {
	return &Registry{factories: map[string]Factory{}}
}

// Register macht eine Handler-Factory unter einem Handler-Typ bekannt (siehe config.ActionHandlerConfig.HandlerType).
func (r *Registry) Register(handlerType string, f Factory) {
	r.factories[handlerType] = f
}

// Activate erzeugt und konfiguriert die Handler aller Aktionen aus der config.yaml, die in mindestens einem
// Projekt angeboten werden. Je Aktion entsteht eine eigene Handler-Instanz, auch wenn mehrere Aktionen denselben
// Handler-Typ nutzen. Bei Aktionen mit projects: [] wird nur geprüft, ob der Handler-Typ existiert.
func (r *Registry) Activate(cfgs []config.ActionHandlerConfig, deps Deps) error {
	var errs []error
	for _, c := range cfgs {
		f, ok := r.factories[c.HandlerType()]
		if !ok {
			errs = append(errs, fmt.Errorf("aktion %q: kein ActionHandler vom Typ %q registriert", c.Name, c.HandlerType()))
			continue
		}
		if !c.Active() {
			slog.Info("Aktion ist deaktiviert (projects ist leer)", "action", c.Name)
			continue
		}
		h := f(deps)
		if err := h.Configure(c.Extra); err != nil {
			errs = append(errs, fmt.Errorf("aktion %q: %w", c.Name, err))
			continue
		}
		r.active = append(r.active, activeAction{cfg: c, handler: h})
	}
	return errors.Join(errs...)
}

// ActionsFor liefert alle auf obj ausführbaren Aktionen (nie nil).
func (r *Registry) ActionsFor(obj StoredObject) []Action {
	result := []Action{}
	for _, a := range r.active {
		if !a.applicable(obj) {
			continue
		}
		result = append(result, Action{
			ID:              a.cfg.Name,
			Title:           a.cfg.Title,
			LucideIcon:      a.cfg.LucideIcon,
			LucideIconColor: a.cfg.LucideIconColor,
			FormFields:      a.handler.FormFields(obj),
		})
	}
	return result
}

// Has meldet, ob die Aktion aktiviert ist.
func (r *Registry) Has(name string) bool {
	_, ok := r.find(name)
	return ok
}

// Execute prüft Applicability und Eingaben und führt die Aktion aus.
func (r *Registry) Execute(ctx context.Context, name string, obj StoredObject, inputs map[string]any) (Result, error) {
	a, ok := r.find(name)
	if !ok {
		return Result{}, ErrUnknownAction
	}
	if !a.applicable(obj) {
		return Result{}, ErrNotApplicable
	}
	if err := validate(a.handler.FormFields(obj), inputs); err != nil {
		return Result{}, err
	}
	return a.handler.Execute(ctx, obj, inputs), nil
}

// applicable prüft zuerst das Projekt (projects aus der config.yaml), dann die Entscheidung des Handlers.
func (a activeAction) applicable(obj StoredObject) bool {
	return slices.Contains(a.cfg.Projects, obj.Project) && a.handler.IsApplicable(obj)
}

func (r *Registry) find(name string) (activeAction, bool) {
	for _, a := range r.active {
		if a.cfg.Name == name {
			return a, true
		}
	}
	return activeAction{}, false
}

func validate(fields []FormField, inputs map[string]any) error {
	var problems []string
	for _, f := range fields {
		v, present := inputs[f.ID]
		if !present || v == nil || v == "" {
			if f.IsRequired {
				problems = append(problems, fmt.Sprintf("%s: Pflichtfeld", f.ID))
			}
			continue
		}
		if msg := checkType(f, v); msg != "" {
			problems = append(problems, fmt.Sprintf("%s: %s", f.ID, msg))
		}
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func checkType(f FormField, v any) string {
	allowed := func(s string) bool {
		return slices.ContainsFunc(f.Options, func(o Option) bool { return o.Value == s })
	}
	switch f.Type {
	case FieldText:
		if _, ok := v.(string); !ok {
			return "Text erwartet"
		}
	case FieldNumber:
		if _, ok := v.(float64); !ok {
			return "Zahl erwartet"
		}
	case FieldSelect:
		if s, ok := v.(string); !ok || !allowed(s) {
			return "ungültige Auswahl"
		}
	case FieldMultiSelect:
		list, ok := v.([]any)
		if !ok {
			return "Liste erwartet"
		}
		if f.IsRequired && len(list) == 0 {
			return "Pflichtfeld"
		}
		for _, item := range list {
			if s, ok := item.(string); !ok || !allowed(s) {
				return "ungültige Auswahl"
			}
		}
	}
	return ""
}
