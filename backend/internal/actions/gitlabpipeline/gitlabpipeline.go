// Package gitlabpipeline implementiert den Handler-Typ „gitlab-pipeline“: Er startet über die Pipeline-Trigger-API
// eine GitLab-Pipeline. Beliebig viele Aktionen können diesen Typ nutzen und unterscheiden sich nur über ihre
// Konfiguration (Projekt, ref, Variablen, Anwendbarkeit). Die Trigger-Logik (Trigger) nutzen auch andere Handler.
package gitlabpipeline

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/model"
	"feature-dashboard/internal/objecttemplate"
)

// HandlerType ist der Wert für `handler:` in der config.yaml.
const HandlerType = "gitlab-pipeline"

// settings sind die extra-handler-configs einer Aktion.
type settings struct {
	TriggerConfig `yaml:",inline"`
	AppliesTo     []model.Kind `yaml:"applies-to"`
	// Projects gibt es nur noch als allgemeines Feld der Aktion; hier nur, um alte Configs zu erkennen.
	Projects []string `yaml:"projects"`
}

// Handler startet eine GitLab-Pipeline.
type Handler struct {
	deps      actions.Deps
	trigger   *Trigger
	appliesTo []model.Kind
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{deps: deps}
}

// Configure liest und prüft die extra-handler-configs. Fehler brechen den Start des Backends ab.
func (h *Handler) Configure(extra map[string]any) error {
	var s settings
	if err := DecodeExtra(extra, &s); err != nil {
		return err
	}

	var errs []error
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
	t, err := NewTrigger(s.TriggerConfig)
	if err != nil {
		errs = append(errs, err)
	} else {
		t.Track(h.deps.Pipelines, h.deps.ActionName)
	}
	h.trigger = t
	return errors.Join(errs...)
}

// DecodeExtra überträgt die extra-handler-configs per YAML-Roundtrip in out (Struct mit yaml-Tags).
func DecodeExtra(extra map[string]any, out any) error {
	raw, err := yaml.Marshal(extra)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("extra-handler-configs: %w", err)
	}
	return nil
}

// IsApplicable prüft den Objekt-Typ (applies-to). Die Projekte prüft bereits die Registry (projects der Aktion).
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	return slices.Contains(h.appliesTo, obj.Ref.Kind)
}

// FormFields: keine Eingaben; das Frontend fragt nur die Bestätigung ab.
func (h *Handler) FormFields(actions.StoredObject) []actions.FormField { return nil }

// Execute löst die Pipeline mit den Daten des Objekts aus.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, _ map[string]any) actions.Result {
	data, err := objecttemplate.NewData(obj.Ref, obj.Object, obj.Project)
	if err != nil {
		return failure("Objekt-Daten nicht lesbar: %v", err)
	}
	return h.trigger.Run(ctx, data)
}
