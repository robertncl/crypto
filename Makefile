# Nebula exchange — developer tasks.
# Go may not be on your PATH; this falls back to the local install location.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)
ADDR ?= :8080

.PHONY: help
help:
	@echo "Nebula exchange — common tasks:"
	@echo "  make deps         Install frontend dependencies"
	@echo "  make backend      Run the API/WS server (port 8080, serves web/dist if built)"
	@echo "  make web          Run the Vite dev server (port 5173, proxies to backend)"
	@echo "  make dev          Reminder of the two-terminal dev workflow"
	@echo "  make build        Build the SPA and a single self-contained ./nebula binary"
	@echo "  make run          Run the built ./nebula binary (serves everything on :8080)"
	@echo "  make test         Run backend tests"
	@echo "  make clean        Remove build artifacts and the local database"

.PHONY: deps
deps:
	cd web && npm install

.PHONY: backend
backend:
	cd backend && ADDR=$(ADDR) $(GO) run ./cmd/server

.PHONY: web
web:
	cd web && npm run dev

.PHONY: dev
dev:
	@echo "Run these in two terminals:"
	@echo "  1) make backend   # Go API + WebSocket on :8080"
	@echo "  2) make web       # Vite SPA on http://localhost:5173"

.PHONY: build-web
build-web:
	cd web && npm run build

.PHONY: build
build: build-web
	cd backend && $(GO) build -o ../nebula ./cmd/server
	@echo "Built ./nebula — run it from the repo root: ./nebula"

.PHONY: run
run:
	./nebula

.PHONY: test
test:
	cd backend && $(GO) test ./...

.PHONY: clean
clean:
	rm -f nebula backend/nebula *.db *.db-shm *.db-wal
	rm -rf web/dist
