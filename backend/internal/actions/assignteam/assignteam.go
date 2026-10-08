// Package assignteam implementiert die Aktion „Team zuordnen“: Sie setzt am Namespace einer Feature-Umgebung das
// Team-Label (teams.label-key) auf die ID eines konfigurierten Teams oder entfernt es.
package assignteam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

// HandlerType ist der Wert für `handler:` bzw. `action-name:` in der config.yaml.
const HandlerType = "assign-team"

// FieldTeam ist die ID des Formular-Felds mit dem gewählten Team.
const FieldTeam = "team"

// NoTeam ist der Auswahlwert „Kein Team“. "-" ist kein gültiger Label-Wert und kann daher keine Team-ID sein.
const NoTeam = "-"

// Handler ordnet einen Namespace einem Team zu.
type Handler struct {
	client kubernetes.Interface
	teams  config.TeamsConfig
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{client: deps.Client, teams: deps.Teams}
}

// Configure: keine extra-handler-configs; die Teams stehen im Abschnitt teams der config.yaml.
func (h *Handler) Configure(map[string]any) error {
	if h.teams.LabelKey == "" || len(h.teams.Teams) == 0 {
		return errors.New("der Abschnitt teams (label-key und teams) muss konfiguriert sein")
	}
	return nil
}

// IsApplicable gilt für Namespaces.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	return obj.Ref.Kind == model.KindNamespace
}

// FormFields: Auswahl des Teams, dazu „Kein Team“ zum Entfernen der Zuordnung.
func (h *Handler) FormFields(obj actions.StoredObject) []actions.FormField {
	options := make([]actions.Option, 0, len(h.teams.Teams)+1)
	for _, t := range h.teams.Teams {
		options = append(options, actions.Option{Value: t.ID, Label: t.Label})
	}
	options = append(options, actions.Option{Value: NoTeam, Label: "Kein Team"})
	return []actions.FormField{{
		ID:          FieldTeam,
		Label:       "Team",
		Type:        actions.FieldSelect,
		IsRequired:  true,
		Description: "Aktuell: " + h.current(obj),
		Options:     options,
	}}
}

// current beschreibt die aktuelle Zuordnung des Namespaces.
func (h *Handler) current(obj actions.StoredObject) string {
	ns, _ := obj.Object.(model.StoredNamespace)
	id, ok := ns.Labels[h.teams.LabelKey]
	if !ok {
		return "kein Team"
	}
	if team, ok := h.teams.Find(id); ok {
		return team.Label
	}
	return fmt.Sprintf("unbekanntes Team %q", id)
}

// Execute setzt oder entfernt das Team-Label per Merge-Patch; andere Labels bleiben unberührt.
// inputs[FieldTeam] ist durch die Registry bereits gegen die Optionen validiert.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, inputs map[string]any) actions.Result {
	id, _ := inputs[FieldTeam].(string)
	var value any = id
	msg := fmt.Sprintf("Namespace %s gehört jetzt zu %s", obj.Ref.Name, id)
	if team, ok := h.teams.Find(id); ok {
		msg = fmt.Sprintf("Namespace %s gehört jetzt zu %s", obj.Ref.Name, team.Label)
	}
	if id == NoTeam {
		value = nil // JSON-Merge-Patch: null entfernt das Label.
		msg = fmt.Sprintf("Namespace %s ist keinem Team mehr zugeordnet", obj.Ref.Name)
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{h.teams.LabelKey: value}}})
	if err != nil {
		return actions.Result{Success: false, Message: err.Error()}
	}
	if _, err := h.client.CoreV1().Namespaces().Patch(ctx, obj.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Team konnte nicht gesetzt werden: %v", err)}
	}
	return actions.Result{Success: true, Message: msg}
}
