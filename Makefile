# Einstiegspunkt für Backend (Go, backend/) und Frontend (Vite, frontend/).
# Relative Pfade in CONFIG und STATIC_DIR beziehen sich auf das Repository-Root.

IMAGE      ?= feature-dashboard-backend:latest
# Optional für make docker hinter dem Firmen-Proxy: Pfad zur Company-CA (PEM). HTTP_PROXY, HTTPS_PROXY und NO_PROXY
# werden aus der Umgebung übernommen, falls gesetzt.
COMPANY_CA ?=
CONFIG     ?= backend/config.example.yaml
STATIC_DIR ?=
KUBECONFIG ?= $(HOME)/.kube/config

BACKEND_BINARY := backend/bin/dashboard
NPM            := npm --prefix frontend

.PHONY: build test lint run docker clean \
	backend-build backend-test backend-vet backend-run \
	frontend-install frontend-dev frontend-build frontend-test frontend-lint

## Sammel-Targets

build: backend-build frontend-build

test: backend-test frontend-test

lint: backend-vet frontend-lint

# Backend lokal starten und den aktuellen Frontend-Build mit ausliefern.
run: frontend-build
	$(MAKE) backend-run STATIC_DIR=frontend/dist

comma := ,
DOCKER_PROXY_ARGS := $(foreach v,HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy,$(if $($(v)),--build-arg $(v)))

docker:
	DOCKER_BUILDKIT=1 docker build $(DOCKER_PROXY_ARGS) \
		$(if $(COMPANY_CA),--secret id=company_ca$(comma)src=$(COMPANY_CA)) -t $(IMAGE) .

clean:
	rm -rf backend/bin frontend/dist

## Backend

backend-build:
	go -C backend build -o bin/dashboard ./cmd/dashboard

backend-test:
	go -C backend test -race ./...

backend-vet:
	go -C backend vet ./...

backend-run:
	go -C backend run ./cmd/dashboard --config "$(abspath $(CONFIG))" --kubeconfig "$(KUBECONFIG)" \
		$(if $(STATIC_DIR),--static-dir "$(abspath $(STATIC_DIR))")

## Frontend

frontend/node_modules: frontend/package.json frontend/package-lock.json
	$(NPM) ci
	@touch $@

frontend-install: frontend/node_modules

frontend-dev: frontend/node_modules
	$(NPM) run dev

frontend-build: frontend/node_modules
	$(NPM) run build

frontend-test: frontend/node_modules
	$(NPM) test

frontend-lint: frontend/node_modules
	$(NPM) run lint
