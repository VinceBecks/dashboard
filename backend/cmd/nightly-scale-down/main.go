// Command nightly-scale-down läuft als Kubernetes-CronJob: Er liest die Namespaces, die im Dashboard vom nächtlichen
// Herunterskalieren ausgenommen wurden, und startet die GitLab-Pipeline, die alle übrigen Feature-Umgebungen auf
// 0 Replicas skaliert. Konfiguration nur über Umgebungsvariablen (siehe scaledown.go).
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/kube"
)

// runTimeout begrenzt den gesamten Lauf, damit der Job nie hängen bleibt.
const runTimeout = 2 * time.Minute

func main() {
	if err := run(); err != nil {
		slog.Error("Nächtliches Herunterskalieren nicht gestartet", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	trigger, err := gitlabpipeline.NewTrigger(cfg.Trigger)
	if err != nil {
		return err
	}
	// Leer: im Cluster die In-Cluster-Konfiguration, lokal KUBECONFIG bzw. ~/.kube/config.
	client, err := kube.NewClient("")
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	pipeline, excluded, err := startScaleDown(ctx, cfg, client, trigger)
	if excluded != nil {
		slog.Info("Ausgenommene Namespaces gelesen", "count", len(excluded), "namespaces", strings.Join(excluded, ","))
	}
	if err != nil {
		return err
	}
	slog.Info("Pipeline für das nächtliche Herunterskalieren gestartet", "id", pipeline.ID, "url", pipeline.WebURL)
	return nil
}
