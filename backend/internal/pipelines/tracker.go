package pipelines

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// ErrNotFound meldet ein StatusChecker, wenn GitLab die Pipeline nicht (mehr) kennt.
var ErrNotFound = errors.New("pipeline nicht gefunden")

// StatusChecker fragt den Status einer Pipeline bei GitLab ab (z. B. "running", "success").
type StatusChecker interface {
	PipelineStatus(ctx context.Context, id int) (string, error)
}

// RegisterChecker meldet für eine Aktion, wie der Status ihrer Pipelines abgefragt wird. Nur Aktionen mit
// Checker legen Einträge an; Einträge von Aktionen ohne Checker entfernt der Tracker.
func (s *State) RegisterChecker(action string, c StatusChecker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkers[action] = c
}

func (s *State) checker(action string) StatusChecker {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkers[action]
}

// Finished meldet, ob eine Pipeline mit diesem GitLab-Status beendet ist.
func Finished(status string) bool {
	switch status {
	case "success", "failed", "canceled", "skipped":
		return true
	}
	return false
}

// Tracker fragt laufende Pipelines regelmäßig bei GitLab ab und entfernt beendete aus dem State.
type Tracker struct {
	state    *State
	interval time.Duration
}

// NewTracker erzeugt einen Tracker, der alle interval prüft.
func NewTracker(state *State, interval time.Duration) *Tracker {
	return &Tracker{state: state, interval: interval}
}

// Run prüft bis ctx endet.
func (t *Tracker) Run(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		t.CheckOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// CheckOnce prüft alle laufenden Pipelines einmal.
func (t *Tracker) CheckOnce(ctx context.Context) {
	for _, e := range t.state.All() {
		if ctx.Err() != nil {
			return
		}
		if !t.finished(ctx, e) {
			continue
		}
		t.state.Remove(e.Ref(), e.Action)
	}
}

// finished meldet, ob der Eintrag entfernt werden soll: Pipeline beendet, unbekannt oder Aktion ohne Checker.
func (t *Tracker) finished(ctx context.Context, e Entry) bool {
	checker := t.state.checker(e.Action)
	if checker == nil {
		slog.Info("Pipeline einer nicht (mehr) verfolgten Aktion wird entfernt", "action", e.Action, "pipeline", e.ID)
		return true
	}
	status, err := checker.PipelineStatus(ctx, e.ID)
	switch {
	case errors.Is(err, ErrNotFound):
		return true
	case err != nil:
		slog.Warn("Pipeline-Status nicht abfragbar", "action", e.Action, "pipeline", e.ID, "error", err)
		return false
	}
	return Finished(status)
}
