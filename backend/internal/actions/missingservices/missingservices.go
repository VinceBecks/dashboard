// Package missingservices implementiert den Handler-Typ „deploy-missing-services“: Er gleicht die konfigurierten
// Services mit den Services eines Namespaces ab, bietet die fehlenden zur Auswahl an und startet für die Auswahl
// eine GitLab-Pipeline.
package missingservices

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// HandlerType ist der Wert für `handler:` bzw. `action-name:` in der config.yaml.
const HandlerType = "deploy-missing-services"

// FieldServices ist die ID des Formular-Felds mit den gewählten Services.
const FieldServices = "services"

type serviceEntry struct {
	Name  string `yaml:"name"`
	Label string `yaml:"label"`
}

// settings sind die extra-handler-configs einer Aktion.
type settings struct {
	gitlabpipeline.TriggerConfig `yaml:",inline"`
	Services                     []serviceEntry `yaml:"services"`
}

// Handler startet eine Pipeline für die im Namespace fehlenden Services.
type Handler struct {
	store    actions.ObjectReader
	services []serviceEntry
	trigger  *gitlabpipeline.Trigger
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{store: deps.Store}
}

// Configure liest und prüft die extra-handler-configs. Fehler brechen den Start des Backends ab.
func (h *Handler) Configure(extra map[string]any) error {
	var s settings
	if err := gitlabpipeline.DecodeExtra(extra, &s); err != nil {
		return err
	}

	var errs []error
	if len(s.Services) == 0 {
		errs = append(errs, errors.New("services fehlt"))
	}
	seen := map[string]bool{}
	for i, e := range s.Services {
		switch {
		case e.Name == "":
			errs = append(errs, fmt.Errorf("services[%d]: name fehlt", i))
		case seen[e.Name]:
			errs = append(errs, fmt.Errorf("services: %q ist doppelt", e.Name))
		}
		seen[e.Name] = true
		if e.Label == "" {
			s.Services[i].Label = e.Name
		}
	}
	h.services = s.Services
	t, err := gitlabpipeline.NewTrigger(s.TriggerConfig)
	if err != nil {
		errs = append(errs, err)
	}
	h.trigger = t
	return errors.Join(errs...)
}

// IsApplicable gilt für alle Namespaces, auch wenn kein Service fehlt (das Dropdown ist dann leer).
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	return obj.Ref.Kind == model.KindNamespace
}

// FormFields bietet die fehlenden Services als Mehrfachauswahl an.
func (h *Handler) FormFields(obj actions.StoredObject) []actions.FormField {
	field := actions.FormField{
		ID:         FieldServices,
		Label:      "Services",
		Type:       actions.FieldMultiSelect,
		IsRequired: true,
		Options:    []actions.Option{},
	}
	missing, err := h.missing(obj.Ref.Namespace)
	switch {
	case err != nil:
		field.Description = fmt.Sprintf("Services des Namespaces nicht lesbar: %v", err)
	case len(missing) == 0:
		field.Description = "Alle konfigurierten Services sind bereits deployt"
	default:
		field.Description = "Fehlende Services, die per Pipeline deployt werden"
		for _, e := range missing {
			field.Options = append(field.Options, actions.Option{Value: e.Name, Label: e.Label})
		}
	}
	return []actions.FormField{field}
}

// Execute startet die Pipeline für die gewählten Services, die weiterhin fehlen.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, inputs map[string]any) actions.Result {
	missing, err := h.missing(obj.Ref.Namespace)
	if err != nil {
		return failure("Services des Namespaces nicht lesbar: %v", err)
	}
	raw, _ := inputs[FieldServices].([]any)
	var selected []string
	for _, v := range raw {
		name, ok := v.(string)
		if !ok || slices.Contains(selected, name) {
			continue
		}
		if !slices.ContainsFunc(missing, func(e serviceEntry) bool { return e.Name == name }) {
			return failure("Service %s fehlt nicht (mehr) im Namespace", name)
		}
		selected = append(selected, name)
	}
	if len(selected) == 0 {
		return failure("Keine fehlenden Services ausgewählt")
	}

	data, err := objecttemplate.NewData(obj.Ref, obj.Object, obj.Project)
	if err != nil {
		return failure("Objekt-Daten nicht lesbar: %v", err)
	}
	data.Services = selected
	return h.trigger.Run(ctx, data)
}

// missing liefert die konfigurierten Services, die im Namespace nicht existieren, in Config-Reihenfolge.
func (h *Handler) missing(namespace string) ([]serviceEntry, error) {
	deployed, err := h.store.Services(namespace)
	if err != nil {
		return nil, err
	}
	var result []serviceEntry
	for _, e := range h.services {
		if !slices.ContainsFunc(deployed, func(s model.StoredService) bool { return s.Name == e.Name }) {
			result = append(result, e)
		}
	}
	return result, nil
}

func failure(format string, args ...any) actions.Result {
	return actions.Result{Success: false, Message: fmt.Sprintf(format, args...)}
}
