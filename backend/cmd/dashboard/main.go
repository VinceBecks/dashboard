// Command dashboard startet das Backend des Feature-Environment-Dashboards.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"feature-dashboard/internal/actions"
	"feature-dashboard/internal/actions/deletepod"
	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/actions/missingservices"
	"feature-dashboard/internal/actions/nodeportservice"
	"feature-dashboard/internal/actions/restartservice"
	"feature-dashboard/internal/api"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/copycommands"
	"feature-dashboard/internal/kube"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/pipelines"
	"feature-dashboard/internal/project"
	"feature-dashboard/internal/store"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Pfad zur config.yaml")
	listen := flag.String("listen", ":8080", "HTTP-Adresse")
	kubeconfig := flag.String("kubeconfig", "", "Pfad zur kubeconfig (leer: In-Cluster, sonst KUBECONFIG/~/.kube/config)")
	staticDir := flag.String("static-dir", "", "Verzeichnis mit dem Frontend-Build (leer: Frontend nicht ausliefern)")
	flag.Parse()

	if err := run(*configPath, *listen, *kubeconfig, *staticDir); err != nil {
		slog.Error("Backend beendet mit Fehler", "error", err)
		os.Exit(1)
	}
}

// pipelinesNamespace liefert den Namespace der Pipeline-ConfigMap: aus der config.yaml, sonst den eigenen
// Namespace (POD_NAMESPACE per Downward-API), sonst „feature-dashboard“.
func pipelinesNamespace(c config.PipelinesConfig) string {
	if c.ConfigMapNamespace != "" {
		return c.ConfigMapNamespace
	}
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	return "feature-dashboard"
}

func run(configPath, listen, kubeconfig, staticDir string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	client, err := kube.NewClient(kubeconfig)
	if err != nil {
		return err
	}

	linkRegistry, err := links.NewRegistry(cfg)
	if err != nil {
		return err
	}
	copyCommandRegistry, err := copycommands.NewRegistry(cfg)
	if err != nil {
		return err
	}
	st := store.New()

	// Laufende Pipelines liegen in einer ConfigMap im Dashboard-Namespace und überstehen so einen Neustart.
	pipelineState := pipelines.NewState(client, pipelinesNamespace(cfg.Pipelines), cfg.Pipelines.ConfigMapName)
	if err := pipelineState.Load(context.Background()); err != nil {
		return err
	}

	registry := actions.NewRegistry()
	// Neue ActionHandler hier registrieren und in der config.yaml aktivieren.
	registry.Register("delete-pod", deletepod.New)
	registry.Register(nodeportservice.CreateActionName, nodeportservice.New)
	registry.Register(nodeportservice.DeleteActionName, nodeportservice.NewDelete)
	registry.Register(gitlabpipeline.HandlerType, gitlabpipeline.New)
	registry.Register(restartservice.ActionName, restartservice.New)
	registry.Register(missingservices.HandlerType, missingservices.New)
	if err := registry.Activate(cfg.ActionHandlers, actions.Deps{Client: client, Store: st, Pipelines: pipelineState}); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	watcher := kube.NewWatcher(client, st, project.NewMatcher(cfg.Projects), cfg.Store.RecreateInterval.Duration())
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		watcher.Run(ctx)
	}()

	go pipelines.NewTracker(pipelineState, cfg.Pipelines.Poll()).Run(ctx)

	apiServer := api.NewServer(st, registry, cfg.Projects, watcher.Ready).WithStaticDir(staticDir).WithLinks(linkRegistry).
		WithCopyCommands(copyCommandRegistry).WithCluster(cfg.Cluster).WithPipelines(pipelineState)
	// Endet ctx, beendet der Hub alle Event-Streams; erst dann kann srv.Shutdown abschließen.
	go apiServer.Run(ctx)
	// Kein WriteTimeout: GET /api/events hält die Verbindung dauerhaft offen.
	srv := &http.Server{
		Addr:              listen,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP-Server gestartet", "address", listen)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErr:
		stop()
		<-watcherDone
		return err
	}
	slog.Info("Fahre herunter")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	<-watcherDone
	return err
}
