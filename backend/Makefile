BINARY := bin/dashboard
IMAGE  ?= feature-dashboard-backend:latest
CONFIG ?= config.example.yaml

.PHONY: build test vet run docker clean

build:
	go build -o $(BINARY) ./cmd/dashboard

test:
	go test -race ./...

vet:
	go vet ./...

run:
	go run ./cmd/dashboard --config $(CONFIG) --kubeconfig "$${KUBECONFIG:-$$HOME/.kube/config}" $(if $(STATIC_DIR),--static-dir $(STATIC_DIR))

docker:
	docker build -f ../Dockerfile -t $(IMAGE) ..

clean:
	rm -rf bin
