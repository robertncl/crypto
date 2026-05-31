# syntax=docker/dockerfile:1
#
# Multi-stage build for the Nebula exchange. Produces a small, static,
# non-root image that serves the REST/WebSocket API and the built SPA on :8080.
#
# Build:  docker build -t nebula .
# Run:    docker run -p 8080:8080 -e JWT_SECRET="$(openssl rand -hex 32)" -v nebula-data:/data nebula
#
# JWT_SECRET (32+ bytes) is required — the server refuses to start without one
# unless DEV=true. See cmd/server/main.go.

############################
# Stage 1 — build the SPA  #
############################
FROM node:26-alpine AS web
WORKDIR /web
# Install deps first against the lockfile for reproducible, cacheable builds.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build   # emits /web/dist (tsc --noEmit && vite build)

##############################
# Stage 2 — build the binary #
##############################
FROM golang:1.26.3 AS build
WORKDIR /src
# Download modules first so they cache independently of source changes.
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# modernc.org/sqlite is pure Go, so CGO is unnecessary — build a fully static
# binary that runs on a distroless/static base.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/nebula ./cmd/server
# Pre-create a data directory we can hand to the non-root runtime user (the
# distroless image has no shell to mkdir at runtime).
RUN mkdir -p /out/data

############################
# Stage 3 — runtime image  #
############################
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/nebula /app/nebula
COPY --from=build --chown=nonroot:nonroot /out/data /data
COPY --from=web /web/dist /app/web/dist
ENV ADDR=:8080 \
    WEB_DIR=/app/web/dist \
    DB_PATH=/data/exchange.db
EXPOSE 8080
USER nonroot:nonroot
VOLUME ["/data"]
ENTRYPOINT ["/app/nebula"]
