// Package nightlyscaledown implementiert den Handler-Typ „nightly-scale-down-exclusion“: Eine externe Anwendung skaliert
// jede Nacht alle Feature-Umgebungen auf 0 Replicas und lässt Namespaces mit dem Label ExclusionLabelKey aus. Der
// Handler setzt dieses Label (mode mark, Namespace ausnehmen) oder entfernt es (mode unmark, wieder herunterskalieren).
// Er wird als zwei Aktionen konfiguriert, die sich gegenseitig ablösen: Markieren wird nur angeboten, solange das
// Label fehlt, Aufheben nur, solange es gesetzt ist.
package nightlyscaledown

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/model"
)

// HandlerType ist der Wert für `handler:` in der config.yaml.
const HandlerType = "nightly-scale-down-exclusion"

// Werte für mode in den extra-handler-configs.
const (
	ModeMark   = "mark"
	ModeUnmark = "unmark"
)

// ExclusionLabelKey und ExclusionLabelValue markieren einen Namespace, der nachts nicht herunterskaliert wird.
// Der CronJob nightly-scale-down sucht die Namespaces mit genau diesem Label.
const (
	ExclusionLabelKey   = "mycomp.de/skip-nightly-scale-down"
	ExclusionLabelValue = "true"
)

// settings sind die extra-handler-configs.
type settings struct {
	Mode string `yaml:"mode"`
}

// Handler nimmt einen Namespace vom nächtlichen Herunterskalieren aus bzw. hebt das wieder auf.
type Handler struct {
	client kubernetes.Interface
	mode   string
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{client: deps.Client}
}

// Configure liest mode (mark | unmark)
func (h *Handler) Configure(extra map[string]any) error {
	var s settings
	if err := gitlabpipeline.DecodeExtra(extra, &s); err != nil {
		return err
	}
	if s.Mode != ModeMark && s.Mode != ModeUnmark {
		return fmt.Errorf("mode muss %q oder %q sein, erhalten: %q", ModeMark, ModeUnmark, s.Mode)
	}
	h.mode = s.Mode
	return nil
}

// IsApplicable gilt für Namespaces: mark, solange das Ausnahme-Label mit ExclusionLabelValue fehlt; unmark, solange
// es gesetzt ist. Ein anderer Wert gilt als nicht gesetzt.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	ns, ok := obj.Object.(model.StoredNamespace)
	if !ok || obj.Ref.Kind != model.KindNamespace {
		return false
	}
	marked := ns.Labels[ExclusionLabelKey] == ExclusionLabelValue
	if h.mode == ModeMark {
		return !marked
	}
	return marked
}

// FormFields: keine Eingaben, nur Bestätigung.
func (h *Handler) FormFields(actions.StoredObject) []actions.FormField {
	return nil
}

// Execute setzt bzw. entfernt das Ausnahme-Label per Merge-Patch; andere Labels bleiben unberührt.
func (h *Handler) Execute(ctx context.Context, obj actions.StoredObject, _ map[string]any) actions.Result {
	var value any = ExclusionLabelValue
	msg := fmt.Sprintf("Namespace %s wird nachts nicht mehr herunterskaliert", obj.Ref.Name)
	if h.mode == ModeUnmark {
		value = nil // JSON-Merge-Patch: null entfernt das Label.
		msg = fmt.Sprintf("Namespace %s wird nachts wieder herunterskaliert", obj.Ref.Name)
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{ExclusionLabelKey: value}}})
	if err != nil {
		return actions.Result{Success: false, Message: err.Error()}
	}
	if _, err := h.client.CoreV1().Namespaces().Patch(ctx, obj.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Ausnahme vom nächtlichen Herunterskalieren konnte für Namespace %s nicht geändert werden (Label %s): %v", obj.Ref.Name, ExclusionLabelKey, err)}
	}
	return actions.Result{Success: true, Message: msg}
}
