// Package scaledowntrigger ist die Logik des CronJobs nightly-scale-down: Er liest die Namespaces, die über das Label
// der Dashboard-Aktion nightly-scale-down-exclusion vom nächtlichen Herunterskalieren ausgenommen sind, und startet
// die GitLab-Pipeline, die das Herunterskalieren ausführt. Die ausgenommenen Namespaces bekommt sie als Input
// EXCLUDE_NAMESPACES (komma-getrennt, sortiert). Konfiguriert wird nur über Umgebungsvariablen.
package scaledowntrigger

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"feature-dashboard/internal/actions/gitlabpipeline"
)

// InputExcludeNamespaces ist der Pipeline-Input (spec:inputs) mit den ausgenommenen Namespaces.
const InputExcludeNamespaces = "EXCLUDE_NAMESPACES"

// Namen der Umgebungsvariablen.
const (
	EnvLabelKey      = "SCALE_DOWN_EXCLUSION_LABEL_KEY"
	EnvLabelValue    = "SCALE_DOWN_EXCLUSION_LABEL_VALUE"
	EnvGitLabURL     = "GITLAB_URL"
	EnvGitLabProject = "GITLAB_PROJECT"
	EnvGitLabRef     = "GITLAB_REF"
	EnvTriggerToken  = "GITLAB_TRIGGER_TOKEN"
	EnvGitLabTimeout = "GITLAB_TIMEOUT"
)

// DefaultLabelValue entspricht dem Standard von label-value der Dashboard-Aktion.
const DefaultLabelValue = "true"

// Config ist die Konfiguration aus den Umgebungsvariablen.
type Config struct {
	LabelKey   string
	LabelValue string
	Ref        string
	Trigger    gitlabpipeline.TriggerConfig
}

// FromEnv liest und prüft die Konfiguration; alle Fehler werden gemeinsam gemeldet. Das Trigger-Token prüft erst
// gitlabpipeline.NewTrigger (es liest EnvTriggerToken selbst).
func FromEnv(getenv func(string) string) (Config, error) {
	c := Config{
		LabelKey:   getenv(EnvLabelKey),
		LabelValue: getenv(EnvLabelValue),
		Ref:        getenv(EnvGitLabRef),
	}
	if c.LabelValue == "" {
		c.LabelValue = DefaultLabelValue
	}
	c.Trigger = gitlabpipeline.TriggerConfig{
		GitLabURL: getenv(EnvGitLabURL),
		Project:   getenv(EnvGitLabProject),
		Ref:       c.Ref,
		TokenEnv:  EnvTriggerToken,
		Timeout:   getenv(EnvGitLabTimeout),
	}

	var errs []error
	for _, name := range []string{EnvLabelKey, EnvGitLabURL, EnvGitLabProject, EnvGitLabRef, EnvTriggerToken} {
		if getenv(name) == "" {
			errs = append(errs, fmt.Errorf("umgebungsvariable %s fehlt", name))
		}
	}
	if c.LabelKey != "" {
		if msgs := validation.IsQualifiedName(c.LabelKey); len(msgs) > 0 {
			errs = append(errs, fmt.Errorf("%s %q ist kein gültiger Label-Key: %s", EnvLabelKey, c.LabelKey, strings.Join(msgs, "; ")))
		}
	}
	if msgs := validation.IsValidLabelValue(c.LabelValue); len(msgs) > 0 {
		errs = append(errs, fmt.Errorf("%s %q ist kein gültiger Label-Wert: %s", EnvLabelValue, c.LabelValue, strings.Join(msgs, "; ")))
	}
	if t := c.Trigger.Timeout; t != "" {
		if d, err := time.ParseDuration(t); err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s %q ist keine gültige Dauer", EnvGitLabTimeout, t))
		}
	}
	return c, errors.Join(errs...)
}

// ExcludedNamespaces liefert die Namen aller Namespaces mit dem Label key=value, sortiert.
func ExcludedNamespaces(ctx context.Context, client kubernetes.Interface, key, value string) ([]string, error) {
	selector := labels.SelectorFromSet(labels.Set{key: value}).String()
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("namespaces mit %s konnten nicht gelesen werden: %w", selector, err)
	}
	names := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		names = append(names, ns.Name)
	}
	slices.Sort(names)
	return names, nil
}

// Starter startet eine Pipeline (erfüllt von *gitlabpipeline.Trigger).
type Starter interface {
	Start(ctx context.Context, ref string, variables, inputs map[string]string) (gitlabpipeline.Pipeline, error)
}

// Run liest die ausgenommenen Namespaces und startet die Pipeline mit ihnen als Input EXCLUDE_NAMESPACES.
func Run(ctx context.Context, cfg Config, client kubernetes.Interface, starter Starter) (gitlabpipeline.Pipeline, []string, error) {
	excluded, err := ExcludedNamespaces(ctx, client, cfg.LabelKey, cfg.LabelValue)
	if err != nil {
		return gitlabpipeline.Pipeline{}, nil, err
	}
	inputs := map[string]string{InputExcludeNamespaces: strings.Join(excluded, ",")}
	pipeline, err := starter.Start(ctx, cfg.Ref, nil, inputs)
	if err != nil {
		return gitlabpipeline.Pipeline{}, excluded, fmt.Errorf("pipeline konnte nicht gestartet werden: %w", err)
	}
	return pipeline, excluded, nil
}
