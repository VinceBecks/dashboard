// Package nightlyscaledown implementiert den Handler-Typ „nightly-scale-down-exclusion“: Eine externe Anwendung skaliert
// jede Nacht alle Feature-Umgebungen auf 0 Replicas und lässt Namespaces mit einem bestimmten Label aus. Der Handler
// setzt dieses Label (mode mark, Namespace ausnehmen) oder entfernt es (mode unmark, wieder herunterskalieren).
// Er wird als zwei Aktionen konfiguriert, die sich gegenseitig ablösen: Markieren wird nur angeboten, solange das
// Label fehlt, Aufheben nur, solange es gesetzt ist.
package nightlyscaledown

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
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

// DefaultLabelValue gilt, wenn label-value fehlt.
const DefaultLabelValue = "true"

// settings sind die extra-handler-configs.
type settings struct {
	LabelKey   string `yaml:"label-key"`
	LabelValue string `yaml:"label-value"`
	Mode       string `yaml:"mode"`
}

// Handler nimmt einen Namespace vom nächtlichen Herunterskalieren aus bzw. hebt das wieder auf.
type Handler struct {
	client kubernetes.Interface
	key    string
	value  string
	mode   string
}

// New ist die Factory für die Registry.
func New(deps actions.Deps) actions.Handler {
	return &Handler{client: deps.Client}
}

// Configure liest label-key und label-value (Standard "true") des Labels, das die Skalierungs-Anwendung auswertet,
// sowie mode (mark | unmark).
func (h *Handler) Configure(extra map[string]any) error {
	var s settings
	if err := gitlabpipeline.DecodeExtra(extra, &s); err != nil {
		return err
	}
	if s.LabelValue == "" {
		s.LabelValue = DefaultLabelValue
	}
	var errs []error
	if s.LabelKey == "" {
		errs = append(errs, errors.New("label-key fehlt"))
	} else if msgs := validation.IsQualifiedName(s.LabelKey); len(msgs) > 0 {
		errs = append(errs, fmt.Errorf("label-key %q ist kein gültiger Label-Key: %s", s.LabelKey, strings.Join(msgs, "; ")))
	}
	if msgs := validation.IsValidLabelValue(s.LabelValue); len(msgs) > 0 {
		errs = append(errs, fmt.Errorf("label-value %q ist kein gültiger Label-Wert: %s", s.LabelValue, strings.Join(msgs, "; ")))
	}
	if s.Mode != ModeMark && s.Mode != ModeUnmark {
		errs = append(errs, fmt.Errorf("mode muss %q oder %q sein, erhalten: %q", ModeMark, ModeUnmark, s.Mode))
	}
	h.key, h.value, h.mode = s.LabelKey, s.LabelValue, s.Mode
	return errors.Join(errs...)
}

// IsApplicable gilt für Namespaces: mark, solange das Label mit dem konfigurierten Wert fehlt; unmark, solange es
// gesetzt ist. Ein anderer Wert gilt als nicht gesetzt.
func (h *Handler) IsApplicable(obj actions.StoredObject) bool {
	ns, ok := obj.Object.(model.StoredNamespace)
	if !ok || obj.Ref.Kind != model.KindNamespace {
		return false
	}
	marked := ns.Labels[h.key] == h.value
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
	var value any = h.value
	msg := fmt.Sprintf("Namespace %s wird nachts nicht mehr herunterskaliert", obj.Ref.Name)
	if h.mode == ModeUnmark {
		value = nil // JSON-Merge-Patch: null entfernt das Label.
		msg = fmt.Sprintf("Namespace %s wird nachts wieder herunterskaliert", obj.Ref.Name)
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{h.key: value}}})
	if err != nil {
		return actions.Result{Success: false, Message: err.Error()}
	}
	if _, err := h.client.CoreV1().Namespaces().Patch(ctx, obj.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return actions.Result{Success: false, Message: fmt.Sprintf("Ausnahme vom nächtlichen Herunterskalieren konnte für Namespace %s nicht geändert werden (Label %s): %v", obj.Ref.Name, h.key, err)}
	}
	return actions.Result{Success: true, Message: msg}
}
