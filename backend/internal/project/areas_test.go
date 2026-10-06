package project

import (
	"reflect"
	"testing"

	"feature-dashboard/internal/config"
)

func TestServiceAreaIDs(t *testing.T) {
	p := config.Project{ServiceAreas: []config.ServiceArea{
		{Name: "business", Label: "Business", LabelCombinations: []map[string]string{
			{"part-of": "shop", "tier": "backend"},
			{"part-of": "billing"},
		}},
		{Name: "infra", Label: "Infrastruktur", LabelCombinations: []map[string]string{{"part-of": "rabbitmq"}}},
		{Name: "monitored", Label: "Überwacht", LabelCombinations: []map[string]string{{"monitored": "true"}}},
	}}
	cases := []struct {
		name   string
		labels map[string]string
		want   []string
	}{
		{"ODER über Kombinationen", map[string]string{"part-of": "billing"}, []string{"business"}},
		{"UND innerhalb einer Kombination", map[string]string{"part-of": "shop", "tier": "backend"}, []string{"business"}},
		{"UND unvollständig → others", map[string]string{"part-of": "shop", "tier": "frontend"}, []string{"others"}},
		{"mehrere Bereiche in Config-Reihenfolge", map[string]string{"part-of": "billing", "monitored": "true"}, []string{"business", "monitored"}},
		{"ohne Labels → others", nil, []string{"others"}},
	}
	for _, c := range cases {
		if got := ServiceAreaIDs(p, c.labels); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if got := ServiceAreaIDs(config.Project{}, map[string]string{"a": "b"}); !reflect.DeepEqual(got, []string{"others"}) {
		t.Errorf("ohne Bereiche: got %v", got)
	}
}
