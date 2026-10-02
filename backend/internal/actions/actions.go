// Package actions enthält das ActionHandler-Interface und die ActionRegistry.
package actions

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"feature-dashboard/internal/model"
)

// Feld-Typen für Formular-Felder.
const (
	FieldText        = "text"
	FieldSelect      = "select"
	FieldMultiSelect = "multi-select"
	FieldNumber      = "number"
)

// StoredObject ist ein Objekt aus dem Store, auf dem eine Aktion ausgeführt werden soll.
// Object ist eine Kopie vom Typ model.StoredNamespace, model.StoredService, model.StoredIngress oder model.StoredPod.
type StoredObject struct {
	Ref    model.ObjectRef
	Object any
}

// FormField beschreibt eine zusätzliche Eingabe, die eine Aktion vom Nutzer benötigt.
type FormField struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	IsRequired  bool     `json:"isRequired"`
	Description string   `json:"description,omitempty"`
	Options     []Option `json:"options,omitempty"`
}

// Option ist ein Auswahlwert für select- und multi-select-Felder.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Result ist das Ergebnis einer Aktion.
type Result struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Handler setzt genau eine Aktion um. Er verändert nur den Cluster, nie den Store;
// der Store wird ausschließlich über die Informer-Events aktualisiert.
type Handler interface {
	// Configure übernimmt die handler-spezifischen extra-handler-configs.
	Configure(extra map[string]any) error
	// IsApplicable entscheidet, ob die Aktion auf obj ausführbar ist.
	IsApplicable(obj StoredObject) bool
	// FormFields liefert die benötigten Eingaben; nil, wenn keine nötig sind.
	FormFields(obj StoredObject) []FormField
	// Execute führt die Aktion aus. inputs sind bereits gegen FormFields validiert.
	Execute(ctx context.Context, obj StoredObject, inputs map[string]any) Result
}

// Deps sind die Abhängigkeiten, die Handler bei ihrer Erzeugung erhalten.
type Deps struct {
	Client kubernetes.Interface
}

// Factory erzeugt einen Handler.
type Factory func(deps Deps) Handler
