# Baut Frontend und Backend in ein Image; das Backend liefert das Frontend unter / aus.
# Build-Kontext ist das Repository-Root (siehe make docker im Ordner backend/).

FROM node:22-alpine AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.26 AS backend
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /dashboard ./cmd/dashboard

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /dashboard /dashboard
COPY --from=frontend /src/dist /static
EXPOSE 8080
ENTRYPOINT ["/dashboard", "--config", "/etc/dashboard/config.yaml", "--static-dir", "/static"]
