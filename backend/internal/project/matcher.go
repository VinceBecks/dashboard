// Package project ordnet Namespaces anhand ihrer Labels einem Projekt zu.
package project

import (
	"log/slog"

	"feature-dashboard/internal/config"
)

// Matcher bestimmt das Projekt eines Namespaces.
type Matcher struct {
	projects []config.Project
}

// NewMatcher erzeugt einen Matcher für die konfigurierten Projekte.
func NewMatcher(projects []config.Project) *Matcher {
	return &Matcher{projects: projects}
}

// Match liefert das erste Projekt (in Config-Reihenfolge), dessen namespace-labels alle gesetzt sind.
func (m *Matcher) Match(nsName string, labels map[string]string) (string, bool) {
	var match string
	for _, p := range m.projects {
		if !hasAll(labels, p.NamespaceLabels) {
			continue
		}
		if match != "" {
			slog.Warn("Namespace passt zu mehreren Projekten, erstes gewinnt",
				"namespace", nsName, "project", match, "ignored", p.Name)
			continue
		}
		match = p.Name
	}
	return match, match != ""
}

func hasAll(labels, required map[string]string) bool {
	for k, v := range required {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}
