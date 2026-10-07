# syntax=docker/dockerfile:1
# Baut Frontend und Backend in ein Image; das Backend liefert das Frontend unter / aus.
# Build-Kontext ist das Repository-Root: make docker
#
# Hinter einem Proxy mit eigener Company-CA (siehe make docker):
# - Proxy: die vordefinierten Build-Args HTTP_PROXY, HTTPS_PROXY, NO_PROXY (npm und go lesen sie aus der Umgebung).
# - CA: als BuildKit-Secret "company_ca" (PEM). Es landet in keinem Image-Layer und wird nur für npm ci und
#   go mod download genutzt. Ohne Secret wird wie bisher gebaut.

FROM node:22-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=secret,id=company_ca,required=false \
    if [ -s /run/secrets/company_ca ]; then export NODE_EXTRA_CA_CERTS=/run/secrets/company_ca; fi; \
    npm ci
COPY frontend/ ./
RUN npm run build

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
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dashboard ./cmd/dashboard

# Zur Laufzeit wird die Company-CA nicht ins Image gebacken, sondern im Cluster eingehängt
# (SSL_CERT_DIR, siehe backend/deploy/network.example.yaml).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /dashboard /dashboard
COPY --from=frontend /src/dist /static
EXPOSE 8080
ENTRYPOINT ["/dashboard", "--config", "/etc/dashboard/config.yaml", "--static-dir", "/static"]
