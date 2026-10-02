package project

import (
	"reflect"
	"testing"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

func svc(name, typ string, labels map[string]string) model.StoredService {
	return model.StoredService{Meta: model.Meta{Name: name, Labels: labels}, Type: typ}
}

func TestAssignServiceAreas(t *testing.T) {
	withAreas := config.Project{ServiceAreas: []config.ServiceArea{
		{Name: "business", Label: "Business", LabelCombinations: []map[string]string{
			{"part-of": "shop", "tier": "backend"},
			{"part-of": "billing"},
		}},
		{Name: "infra", Label: "Infrastruktur", LabelCombinations: []map[string]string{{"part-of": "rabbitmq"}}},
		{Name: "monitored", Label: "Überwacht", LabelCombinations: []map[string]string{{"monitored": "true"}}},
		{Name: "empty", Label: "Leer", LabelCombinations: []map[string]string{{"never": "x"}}},
	}}
	services := []model.StoredService{
		svc("billing", "ClusterIP", map[string]string{"part-of": "billing", "monitored": "true"}),
		svc("debug", "ClusterIP", nil),
		svc("mq", "ClusterIP", map[string]string{"part-of": "rabbitmq"}),
		svc("shop-api", "ClusterIP", map[string]string{"part-of": "shop", "tier": "backend"}),
		svc("shop-ui", "ClusterIP", map[string]string{"part-of": "shop", "tier": "frontend"}),
		svc("shop-api-node-port", "NodePort", map[string]string{"part-of": "shop", "tier": "backend"}),
	}

	cases := []struct {
		name     string
		project  config.Project
		services []model.StoredService
		want     []Area
	}{
		{
			name: "UND/ODER, Mehrfach-Zuordnung, others zuletzt, NodePort ignoriert", project: withAreas, services: services,
			want: []Area{
				{ID: "business", Label: "Business", Services: []string{"billing", "shop-api"}},
				{ID: "infra", Label: "Infrastruktur", Services: []string{"mq"}},
				{ID: "monitored", Label: "Überwacht", Services: []string{"billing"}},
				{ID: "empty", Label: "Leer", Services: []string{}},
				{ID: "others", Label: "Andere", Services: []string{"debug", "shop-ui"}},
			},
		},
		{
			name: "others entfällt, wenn alles zugeordnet ist", project: withAreas, services: services[2:3],
			want: []Area{
				{ID: "business", Label: "Business", Services: []string{}},
				{ID: "infra", Label: "Infrastruktur", Services: []string{"mq"}},
				{ID: "monitored", Label: "Überwacht", Services: []string{}},
				{ID: "empty", Label: "Leer", Services: []string{}},
			},
		},
		{
			name: "ohne Bereiche: alles in others", project: config.Project{}, services: services,
			want: []Area{{ID: "others", Label: "Anwendungs-Services", Services: []string{"billing", "debug", "mq", "shop-api", "shop-ui"}}},
		},
		{
			name: "ohne Bereiche und Services: leerer others-Bereich", project: config.Project{OthersAreaLabel: "Apps"},
			want: []Area{{ID: "others", Label: "Apps", Services: []string{}}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AssignServiceAreas(c.project, c.services); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got  %+v\nwant %+v", got, c.want)
			}
		})
	}
}
