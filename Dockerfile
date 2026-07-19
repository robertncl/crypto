# syntax=docker/dockerfile:1
#
# Multi-stage build for the Nebula exchange on Chainguard images (minimal,
# low/zero-CVE, nonroot). Produces a small static image that serves the
# REST/WebSocket API and the built SPA on :8080.
#
# Build:  docker build -t nebula .
# Run:    docker run -p 8080:8080 -e JWT_SECRET="$(openssl rand -hex 32)" -v nebula-data:/data nebula
#
# JWT_SECRET (32+ bytes) is required — the server refuses to start without one
# unless DEV=true. See cmd/server/main.go.

############################
# Stage 1 — build the SPA  #
############################
# -dev variant includes a shell + npm for the build; it is discarded.
FROM cgr.dev/chainguard/node:latest-dev@sha256:7f240e0b8a76496e6128948e4cfb0c3c145f629ac2b9d3cee3d554b746e82ca3 AS web
USER root
WORKDIR /web
# Install deps first against the lockfile for reproducible, cacheable builds.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build   # emits /web/dist (tsc --noEmit && vite build)

##############################
# Stage 2 — build the binary #
##############################
FROM cgr.dev/chainguard/go:latest-dev@sha256:041a53344f009008fdd8ec3d2dba43cc19a83d2b856635f802b09c8e76552b23 AS build
USER root
WORKDIR /src
# Honor the toolchain pinned in go.mod even if the base ships a different Go.
ENV GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux
# Download modules first so they cache independently of source changes.
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# modernc.org/sqlite is pure Go, so CGO is unnecessary — build a fully static
# binary that runs on the minimal static base.
RUN go build -trimpath -ldflags="-s -w" -o /out/nebula ./cmd/server
# Pre-create a data directory we can hand to the nonroot runtime user (the
# static image has no shell to mkdir at runtime).
RUN mkdir -p /out/data

############################
# Stage 3 — runtime image  #
############################
# Chainguard static: distroless-equivalent, nonroot (uid 65532), with CA certs.
FROM cgr.dev/chainguard/static:latest@sha256:60582b2ae6074f641094af0f370d4ab241aab271858a66223dcde7eee9f51638
WORKDIR /app
COPY --from=build /out/nebula /app/nebula
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=web /web/dist /app/web/dist
ENV ADDR=:8080 \
    WEB_DIR=/app/web/dist \
    DB_PATH=/data/exchange.db
EXPOSE 8080
USER 65532:65532
VOLUME ["/data"]
ENTRYPOINT ["/app/nebula"]
