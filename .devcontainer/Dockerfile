FROM cgr.dev/chainguard/go:latest-dev

USER root

# Wolfi (apk) packages: Node.js + npm for the frontend, git for VCS tools.
# posix-libc-utils supplies getconf, which VS Code Server's check-requirements.sh
# calls at startup to detect the architecture — without it the server refuses
# to start with exit 127.
RUN apk add --no-cache nodejs npm git bash posix-libc-utils

# Create a non-root dev user (uid 1000) that VS Code / Dev Containers expects
RUN adduser -D -u 1000 -s /bin/bash vscode

# Pre-create workspace and data directories owned by the dev user.
# node_modules is created here so Docker initialises the named volume with the
# correct ownership (Docker copies image-directory ownership on first mount).
RUN mkdir -p /workspace/web/node_modules /data && chown -R 1000:1000 /workspace /data

USER vscode
WORKDIR /workspace

ENV GOPATH=/home/vscode/go \
    GOMODCACHE=/home/vscode/go/pkg/mod \
    PATH="/home/vscode/go/bin:/usr/local/go/bin:${PATH}" \
    CGO_ENABLED=0

# Pre-create Go cache dirs as vscode so the named volumes inherit that ownership.
RUN mkdir -p /home/vscode/go/pkg/mod /home/vscode/go/bin
