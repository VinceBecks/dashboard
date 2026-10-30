package main

// Logik des CronJobs: Er liest die Namespaces, die über das feste Label der Dashboard-Aktion
// nightly-scale-down-exclusion (nightlyscaledown.ExclusionLabelKey) vom nächtlichen Herunterskalieren ausgenommen sind, und startet die GitLab-Pipeline, die das Herunterskalieren
// ausführt. Die ausgenommenen Namespaces bekommt sie als Input EXCLUDE_NAMESPACES (komma-getrennt, sortiert).
// Konfiguriert wird nur über Umgebungsvariablen. Der Code gehört nur zu dieser Anwendung, nicht zum Dashboard.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

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
)

// config ist die Konfiguration aus den Umgebungsvariablen.
type config struct {
	Ref     string
	Trigger gitlabpipeline.TriggerConfig
}

// configFromEnv liest und prüft die Konfiguration; alle Fehler werden gemeinsam gemeldet. Das Trigger-Token prüft erst
// gitlabpipeline.NewTrigger (es liest EnvTriggerToken selbst).
func configFromEnv(getenv func(string) string) (config, error) {
	c := config{Ref: getenv(EnvGitLabRef)}
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

// starter startet eine Pipeline (erfüllt von *gitlabpipeline.Trigger).
type starter interface {
	Start(ctx context.Context, ref string, variables, inputs map[string]string) (gitlabpipeline.Pipeline, error)
}

// startScaleDown liest die ausgenommenen Namespaces und startet die Pipeline mit ihnen als Input EXCLUDE_NAMESPACES.
func startScaleDown(ctx context.Context, cfg config, client kubernetes.Interface, st starter) (gitlabpipeline.Pipeline, []string, error) {
	excluded, err := excludedNamespaces(ctx, client)
	if err != nil {
		return gitlabpipeline.Pipeline{}, nil, err
	}
	inputs := map[string]string{InputExcludeNamespaces: strings.Join(excluded, ",")}
	pipeline, err := st.Start(ctx, cfg.Ref, nil, inputs)
	if err != nil {
		return gitlabpipeline.Pipeline{}, excluded, fmt.Errorf("pipeline konnte nicht gestartet werden: %w", err)
	}
	return pipeline, excluded, nil
}
