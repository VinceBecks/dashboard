package labelselector

import "testing"

func TestMatchesAny(t *testing.T) {
	combos := []map[string]string{
		{"part-of": "shop", "tier": "backend"},
		{"part-of": "billing"},
	}
	cases := []struct {
		name   string
		labels map[string]string
		want   bool
	}{
		{"ODER über Kombinationen", map[string]string{"part-of": "billing"}, true},
		{"UND innerhalb einer Kombination", map[string]string{"part-of": "shop", "tier": "backend"}, true},
		{"UND unvollständig", map[string]string{"part-of": "shop", "tier": "frontend"}, false},
		{"ohne Labels", nil, false},
	}
	for _, c := range cases {
		if got := MatchesAny(c.labels, combos); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if MatchesAny(map[string]string{"a": "b"}, []map[string]string{{}}) {
		t.Error("leere Kombination darf nie treffen")
	}
	if MatchesAny(map[string]string{"a": "b"}, nil) {
		t.Error("keine Kombinationen dürfen nie treffen")
	}
}

func TestValidate(t *testing.T) {
	if errs := Validate("x", []map[string]string{{"a": "b"}}); len(errs) != 0 {
		t.Errorf("gültig: %v", errs)
	}
	if errs := Validate("x", nil); len(errs) != 1 {
		t.Errorf("leer: %v", errs)
	}
	if errs := Validate("x", []map[string]string{{"a": "b"}, {}}); len(errs) != 1 {
		t.Errorf("leerer Eintrag: %v", errs)
	}
}
