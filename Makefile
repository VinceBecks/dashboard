# Einstiegspunkt für das Backend (Go, backend/).
# Relative Pfade in CONFIG und STATIC_DIR beziehen sich auf das Repository-Root.

IMAGE      ?= feature-dashboard-backend:latest
# Optional für make docker hinter dem Firmen-Proxy: Pfad zur Company-CA (PEM). HTTP_PROXY, HTTPS_PROXY und NO_PROXY
# werden aus der Umgebung übernommen, falls gesetzt.
COMPANY_CA ?=
CONFIG     ?= backend/config.example.yaml
# Optional: Verzeichnis mit statischen Dateien, die das Backend unter / ausliefert (--static-dir).
STATIC_DIR ?=
KUBECONFIG ?= $(HOME)/.kube/config

.PHONY: build test lint run docker clean \
	backend-build backend-test backend-vet backend-run nightly-scale-down-run

## Sammel-Targets

build: backend-build

test: backend-test

lint: backend-vet

# Backend lokal starten.
run: backend-run

comma := ,
DOCKER_PROXY_ARGS := $(foreach v,HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy,$(if $($(v)),--build-arg $(v)))

docker:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_PROXY_ARGS) \
		$(if $(COMPANY_CA),--secret id=company_ca$(comma)src=$(COMPANY_CA)) -t $(IMAGE) .

clean:
	rm -rf backend/bin

## Backend

# Baut alle Binaries nach backend/bin/: dashboard und nightly-scale-down.
backend-build:
	go -C backend build -o bin/ ./cmd/...

backend-test:
	go -C backend test -race ./...

backend-vet:
	go -C backend vet ./...

backend-run:
	go -C backend run ./cmd/dashboard --config "$(abspath $(CONFIG))" --kubeconfig "$(KUBECONFIG)" \
		$(if $(STATIC_DIR),--static-dir "$(abspath $(STATIC_DIR))")

# CronJob-Anwendung einmal lokal ausführen; Konfiguration nur über Umgebungsvariablen (siehe README).
nightly-scale-down-run:
	KUBECONFIG="$(KUBECONFIG)" go -C backend run ./cmd/nightly-scale-down
