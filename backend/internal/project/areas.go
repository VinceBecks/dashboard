package project

import (
	corev1 "k8s.io/api/core/v1"

	"feature-dashboard/internal/config"
	"feature-dashboard/internal/model"
)

// Area ist ein Bereich von Anwendungs-Services mit den Namen der zugeordneten Services.
type Area struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Services []string `json:"services"`
}

// AssignServiceAreas verteilt die Anwendungs-Services (ClusterIP) auf die Bereiche des Projekts.
//
// Ein Service kann in mehreren Bereichen liegen. Die Bereiche kommen in Config-Reihenfolge und werden auch leer
// geliefert. Nicht zugeordnete Services landen im Bereich „others“, der zuletzt kommt und nur geliefert wird, wenn
// er Services enthält oder das Projekt keine Bereiche konfiguriert hat. Die Service-Reihenfolge bleibt erhalten.
func AssignServiceAreas(p config.Project, services []model.StoredService) []Area {
	areas := make([]Area, 0, len(p.ServiceAreas)+1)
	for _, a := range p.ServiceAreas {
		areas = append(areas, Area{ID: a.Name, Label: a.Label, Services: []string{}})
	}
	others := Area{ID: config.OthersAreaName, Label: p.OthersLabel(), Services: []string{}}

	for _, svc := range services {
		if svc.Type != string(corev1.ServiceTypeClusterIP) {
			continue
		}
		assigned := false
		for i, a := range p.ServiceAreas {
			if matchesAny(svc.Labels, a.LabelCombinations) {
				areas[i].Services = append(areas[i].Services, svc.Name)
				assigned = true
			}
		}
		if !assigned {
			others.Services = append(others.Services, svc.Name)
		}
	}

	if len(others.Services) > 0 || len(p.ServiceAreas) == 0 {
		areas = append(areas, others)
	}
	return areas
}

func matchesAny(labels map[string]string, combinations []map[string]string) bool {
	for _, c := range combinations {
		if len(c) > 0 && hasAll(labels, c) {
			return true
		}
	}
	return false
}
