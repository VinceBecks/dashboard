# syntax=docker/dockerfile:1
# Baut das Backend-Image mit zwei Binaries: /dashboard (Deployment, ENTRYPOINT) und /nightly-scale-down (CronJob,
# per command). Build-Kontext ist das Repository-Root: make docker
#
# Hinter einem Proxy mit eigener Company-CA (siehe make docker):
# - Proxy: die vordefinierten Build-Args HTTP_PROXY, HTTPS_PROXY, NO_PROXY (go liest sie aus der Umgebung).
# - CA: als BuildKit-Secret "company_ca" (PEM). Es landet in keinem Image-Layer und wird nur für
#   go mod download genutzt. Ohne Secret wird wie bisher gebaut.

FROM golang:1.26 AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=secret,id=company_ca,required=false \
    if [ -s /run/secrets/company_ca ]; then \
      cat /etc/ssl/certs/ca-certificates.crt /run/secrets/company_ca > /tmp/ca-bundle.crt; \
      export SSL_CERT_FILE=/tmp/ca-bundle.crt; \
    fi; \
    go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dashboard ./cmd/dashboard && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /nightly-scale-down ./cmd/nightly-scale-down

# Zur Laufzeit wird die Company-CA nicht ins Image gebacken, sondern im Cluster eingehängt
# (SSL_CERT_DIR, siehe backend/deploy/network.example.yaml).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /dashboard /dashboard
COPY --from=backend /nightly-scale-down /nightly-scale-down
EXPOSE 8080
ENTRYPOINT ["/dashboard", "--config", "/etc/dashboard/config.yaml"]
