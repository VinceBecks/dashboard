package main

// Logik des CronJobs: Er liest die Namespaces, die über das feste Label der Dashboard-Aktion
// nightly-scale-down-exclusion (nightlyscaledown.ExclusionLabelKey) vom nächtlichen Herunterskalieren ausgenommen sind, und startet die GitLab-Pipeline, die das Herunterskalieren
// ausführt. Die ausgenommenen Namespaces bekommt sie als Input EXCLUDE_NAMESPACES (komma-getrennt, sortiert).
// Nach dem erfolgreichen Start entfernt er das Label wieder: Eine Ausnahme gilt nur für einen Lauf.
// Optional entfernt er vor der Übergabe ein Präfix von den Namen (TRIM_NAMESPACE_PREFIX).
// Konfiguriert wird nur über Umgebungsvariablen. Der Code gehört nur zu dieser Anwendung, nicht zum Dashboard.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/actions/nightlyscaledown"
)

// InputExcludeNamespaces ist der Pipeline-Input (spec:inputs) mit den ausgenommenen Namespaces.
const InputExcludeNamespaces = "EXCLUDE_NAMESPACES"

// Namen der Umgebungsvariablen.
const (
	EnvGitLabURL     = "GITLAB_URL"
	EnvGitLabProject = "GITLAB_PROJECT"
	EnvGitLabRef     = "GITLAB_REF"
	EnvTriggerToken  = "GITLAB_TRIGGER_TOKEN"
	EnvGitLabTimeout = "GITLAB_TIMEOUT"
	// EnvTrimNamespacePrefix ist optional: Präfix, das von den Namen in EXCLUDE_NAMESPACES entfernt wird.
	EnvTrimNamespacePrefix = "TRIM_NAMESPACE_PREFIX"
)

// config ist die Konfiguration aus den Umgebungsvariablen.
type config struct {
	Ref     string
	Trigger gitlabpipeline.TriggerConfig
	// TrimNamespacePrefix wird von den Namen im Pipeline-Input entfernt; leer = nicht kürzen.
	TrimNamespacePrefix string
}

// configFromEnv liest und prüft die Konfiguration; alle Fehler werden gemeinsam gemeldet. Das Trigger-Token prüft erst
// gitlabpipeline.NewTrigger (es liest EnvTriggerToken selbst).
func configFromEnv(getenv func(string) string) (config, error) {
	c := config{Ref: getenv(EnvGitLabRef), TrimNamespacePrefix: getenv(EnvTrimNamespacePrefix)}
	c.Trigger = gitlabpipeline.TriggerConfig{
		GitLabURL: getenv(EnvGitLabURL),
		Project:   getenv(EnvGitLabProject),
		Ref:       c.Ref,
		TokenEnv:  EnvTriggerToken,
		Timeout:   getenv(EnvGitLabTimeout),
	}

	var errs []error
	for _, name := range []string{EnvGitLabURL, EnvGitLabProject, EnvGitLabRef, EnvTriggerToken} {
		if getenv(name) == "" {
			errs = append(errs, fmt.Errorf("umgebungsvariable %s fehlt", name))
		}
	}
	if t := c.Trigger.Timeout; t != "" {
		if d, err := time.ParseDuration(t); err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("%s %q ist keine gültige Dauer", EnvGitLabTimeout, t))
		}
	}
	return c, errors.Join(errs...)
}

// excludedNamespaces liefert die Namen aller Namespaces mit dem Ausnahme-Label der Dashboard-Aktion, sortiert.
func excludedNamespaces(ctx context.Context, client kubernetes.Interface) ([]string, error) {
	selector := labels.SelectorFromSet(labels.Set{nightlyscaledown.ExclusionLabelKey: nightlyscaledown.ExclusionLabelValue}).String()
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

// pipelineNamespaces liefert die Namen für den Pipeline-Input: prefix entfernt (Namen ohne prefix bleiben
// unverändert), leere Namen weggelassen, sortiert und ohne Duplikate.
func pipelineNamespaces(names []string, prefix string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if trimmed := strings.TrimPrefix(name, prefix); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// starter startet eine Pipeline (erfüllt von *gitlabpipeline.Trigger).
type starter interface {
	Start(ctx context.Context, ref string, variables, inputs map[string]string) (gitlabpipeline.Pipeline, error)
}

// removeExclusions entfernt das Ausnahme-Label an den Namespaces per JSON-Merge-Patch; andere Labels bleiben
// unberührt. Alle Namespaces werden versucht, Fehler gesammelt gemeldet. Inzwischen gelöschte Namespaces sind kein
// Fehler.
func removeExclusions(ctx context.Context, client kubernetes.Interface, names []string) error {
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{nightlyscaledown.ExclusionLabelKey: nil}}})
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		_, err := client.CoreV1().Namespaces().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("label %s konnte an Namespace %s nicht entfernt werden: %w", nightlyscaledown.ExclusionLabelKey, name, err))
		}
	}
	return errors.Join(errs...)
}

// errRemoveExclusions kennzeichnet Fehler beim Entfernen des Labels; die Pipeline läuft dann bereits.
var errRemoveExclusions = errors.New("ausnahme-label konnte nicht entfernt werden")

// startScaleDown liest die ausgenommenen Namespaces, startet die Pipeline mit ihnen als Input EXCLUDE_NAMESPACES
// (ohne cfg.TrimNamespacePrefix, siehe pipelineNamespaces) und
// entfernt danach das Ausnahme-Label, damit die Namespaces beim nächsten Lauf wieder herunterskaliert werden.
// Schlägt der Start fehl, bleiben die Labels. Schlägt nur das Entfernen fehl, kommt die gestartete Pipeline zusammen
// mit einem Fehler zurück, der errRemoveExclusions enthält.
func startScaleDown(ctx context.Context, cfg config, client kubernetes.Interface, st starter) (gitlabpipeline.Pipeline, []string, error) {
	excluded, err := excludedNamespaces(ctx, client)
	if err != nil {
		return gitlabpipeline.Pipeline{}, nil, err
	}
	inputs := map[string]string{InputExcludeNamespaces: strings.Join(pipelineNamespaces(excluded, cfg.TrimNamespacePrefix), ",")}
	pipeline, err := st.Start(ctx, cfg.Ref, nil, inputs)
	if err != nil {
		return gitlabpipeline.Pipeline{}, excluded, fmt.Errorf("pipeline konnte nicht gestartet werden: %w", err)
	}
	// Das Label sitzt an den echten Namespaces, daher die ungekürzten Namen.
	if err := removeExclusions(ctx, client, excluded); err != nil {
		return pipeline, excluded, errors.Join(errRemoveExclusions, err)
	}
	return pipeline, excluded, nil
}
