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
	"feature-dashboard/internal/actions/assignteam"
	"feature-dashboard/internal/actions/deletepod"
	"feature-dashboard/internal/actions/gitlabpipeline"
	"feature-dashboard/internal/actions/missingservices"
	"feature-dashboard/internal/actions/nightlyscaledown"
	"feature-dashboard/internal/actions/nodeportservice"
	"feature-dashboard/internal/actions/restartservice"
	"feature-dashboard/internal/api"
	"feature-dashboard/internal/config"
	"feature-dashboard/internal/copycommands"
	"feature-dashboard/internal/kube"
	"feature-dashboard/internal/links"
	"feature-dashboard/internal/pipelines"
	"feature-dashboard/internal/project"
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
	// Einzige Datenquelle ist der Informer-Cache; es gibt keine eigene Kopie der Objekte.
	cluster := kube.NewCluster(client, project.NewMatcher(cfg.Projects))

	// Laufende Pipelines liegen nur im Speicher; nach einem Neustart ist der Stand leer.
	pipelineState := pipelines.NewState()

	registry := actions.NewRegistry()
	// Neue ActionHandler hier registrieren und in der config.yaml aktivieren.
	registry.Register("delete-pod", deletepod.New)
	registry.Register(nodeportservice.CreateActionName, nodeportservice.New)
	registry.Register(nodeportservice.DeleteActionName, nodeportservice.NewDelete)
	registry.Register(gitlabpipeline.HandlerType, gitlabpipeline.New)
	registry.Register(restartservice.ActionName, restartservice.New)
	registry.Register(missingservices.HandlerType, missingservices.New)
	registry.Register(assignteam.HandlerType, assignteam.New)
	registry.Register(nightlyscaledown.HandlerType, nightlyscaledown.New)
	if err := registry.Activate(cfg.ActionHandlers, actions.Deps{Client: client, Store: cluster, Pipelines: pipelineState, Teams: cfg.Teams}); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	clusterDone := make(chan struct{})
	go func() {
		defer close(clusterDone)
		cluster.Run(ctx)
	}()

	go pipelines.NewTracker(pipelineState, cfg.Pipelines.Poll()).Run(ctx)

	apiServer := api.NewServer(cluster, registry, cfg.Projects, cluster.Ready).WithStaticDir(staticDir).WithLinks(linkRegistry).
		WithCopyCommands(copyCommandRegistry).WithCluster(cfg.Cluster).WithPipelines(pipelineState).WithTeams(cfg.Teams)
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
		<-clusterDone
		return err
	}
	slog.Info("Fahre herunter")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	<-clusterDone
	return err
}
