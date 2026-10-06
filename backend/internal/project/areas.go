package project

import "feature-dashboard/internal/config"

// ServiceAreaIDs liefert die IDs aller Bereiche des Projekts, zu denen ein Service mit diesen Labels gehört, in
// Config-Reihenfolge. Ein Service kann in mehreren Bereichen liegen; passt keiner, gehört er zu „others“.
func ServiceAreaIDs(p config.Project, labels map[string]string) []string {
	var ids []string
	for _, a := range p.ServiceAreas {
		if matchesAny(labels, a.LabelCombinations) {
			ids = append(ids, a.Name)
		}
	}
	if len(ids) == 0 {
		return []string{config.OthersAreaName}
	}
	return ids
}

// matchesAny gilt, wenn labels alle Labels mindestens einer nicht leeren Kombination tragen.
func matchesAny(labels map[string]string, combinations []map[string]string) bool {
	for _, c := range combinations {
		if len(c) > 0 && hasAll(labels, c) {
			return true
		}
	}
	return false
}
