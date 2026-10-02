package kube

import "context"

// rebuildForTest stößt einen Full-Rebuild an. Läuft im Run-Loop, damit Events und Rebuild serialisiert bleiben.
func (w *Watcher) rebuildForTest(ctx context.Context) error {
	done := make(chan error, 1)
	w.commands <- func() { done <- w.rebuild(ctx) }
	return <-done
}
