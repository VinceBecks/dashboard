// Package labelselector gleicht Labels gegen Label-Selektoren ab: eine Liste von Kombinationen, ODER über die
// Einträge, UND innerhalb eines Eintrags.
package labelselector

import "fmt"

// HasAll gilt, wenn labels alle Einträge von required mit gleichem Wert tragen.
func HasAll(labels, required map[string]string) bool {
	for k, v := range required {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}

// MatchesAny gilt, wenn labels alle Labels mindestens einer nicht leeren Kombination tragen.
func MatchesAny(labels map[string]string, combinations []map[string]string) bool {
	for _, c := range combinations {
		if len(c) > 0 && HasAll(labels, c) {
			return true
		}
	}
	return false
}

// Validate prüft, dass combinations nicht leer ist und keine leere Kombination enthält (sie würde alles treffen).
// path benennt die Config-Stelle in den Fehlermeldungen.
func Validate(path string, combinations []map[string]string) []error {
	var errs []error
	if len(combinations) == 0 {
		errs = append(errs, fmt.Errorf("%s: dürfen nicht leer sein", path))
	}
	for i, c := range combinations {
		if len(c) == 0 {
			errs = append(errs, fmt.Errorf("%s[%d]: leere Kombination würde alle Services treffen", path, i))
		}
	}
	return errs
}
