package project

import (
	"testing"

	"feature-dashboard/internal/config"
)

func TestMatch(t *testing.T) {
	m := NewMatcher([]config.Project{
		{Name: "foo", NamespaceLabels: map[string]string{"env": "true", "project": "foo"}},
		{Name: "bar", NamespaceLabels: map[string]string{"env": "true", "project": "bar"}},
	})
	if p, ok := m.Match("ns", map[string]string{"env": "true", "project": "bar", "x": "y"}); !ok || p != "bar" {
		t.Errorf("got %q %v", p, ok)
	}
	if _, ok := m.Match("ns", map[string]string{"env": "true"}); ok {
		t.Error("kein Treffer erwartet")
	}
	if _, ok := m.Match("ns", map[string]string{"env": "false", "project": "foo"}); ok {
		t.Error("kein Treffer bei abweichendem Wert erwartet")
	}
}
