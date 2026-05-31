# Nebula exchange — developer tasks.
# Go may not be on your PATH; this falls back to the local install location.
GO ?= $(shell command -v go 2>/dev/null || echo $(HOME)/.local/go/bin/go)
ADDR ?= :8080

# The server refuses the insecure default JWT secret unless DEV=true. By default
# the run targets use local dev mode. Pass a real secret to exercise the secured
# production path instead, e.g.:
#   make run JWT_SECRET=$(openssl rand -hex 32)
JWT_SECRET ?=
ifeq ($(strip $(JWT_SECRET)),)
RUN_ENV := DEV=true
else
RUN_ENV := JWT_SECRET=$(JWT_SECRET)
endif

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
	@echo ""
	@echo "  backend/run use local dev mode by default. To run the secured path:"
	@echo "    make run JWT_SECRET=\$$(openssl rand -hex 32)"

.PHONY: deps
deps:
	cd web && npm install

.PHONY: backend
backend:
	cd backend && $(RUN_ENV) ADDR=$(ADDR) $(GO) run ./cmd/server

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
	$(RUN_ENV) ./nebula

.PHONY: test
test:
	cd backend && $(GO) test ./...

.PHONY: clean
clean:
	rm -f nebula backend/nebula *.db *.db-shm *.db-wal
	rm -rf web/dist
