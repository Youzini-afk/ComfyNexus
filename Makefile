# --- ComfyNexus Makefile ---
.PHONY: help dev dev-web dev-api build build-web build-api test test-go test-web quality lint fmt tidy clean docker docker-run gen-key

GO            ?= go
NPM           ?= npm
BINARY        := comfynexus
DIST          := ./dist
WEB_DIR       := ./web
GO_PKG        := ./...

help: ## Show this help
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*##/ {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

dev: ## Run API and web dev servers in parallel (requires foreman or run separately)
	@echo "Run 'make dev-api' and 'make dev-web' in two terminals."

dev-api: ## Run Go API in dev mode
	$(GO) run ./cmd/comfynexus

dev-web: ## Run Vite dev server
	cd $(WEB_DIR) && $(NPM) run dev

build: build-web build-api ## Build everything (web first, then Go binary with embedded assets)

build-web: ## Build the Vite frontend into web/dist
	cd $(WEB_DIR) && $(NPM) ci --include=dev && $(NPM) run build

build-api: ## Build the Go binary with embedded web assets
	mkdir -p $(DIST)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w" -o $(DIST)/$(BINARY) ./cmd/comfynexus

test: test-go test-web ## Run all tests

test-go: ## Run Go tests
	$(GO) test -race -count=1 $(GO_PKG)

test-web: ## Run frontend tests (no-op until vitest is configured)
	cd $(WEB_DIR) && $(NPM) test --if-present

quality: test-go build-web ## Run backend tests and verify the frontend production build

lint: ## Run linters
	$(GO) vet $(GO_PKG)
	cd $(WEB_DIR) && $(NPM) run lint --if-present

fmt: ## Format Go code
	$(GO) fmt $(GO_PKG)

tidy: ## Tidy go.mod
	$(GO) mod tidy

clean: ## Remove build artifacts
	rm -rf $(DIST) $(WEB_DIR)/dist $(WEB_DIR)/node_modules

docker: ## Build container image
	docker build -t comfynexus:dev .

docker-run: docker ## Build and run the container locally
	docker run --rm -it -p 8080:8080 -e COMFYNEXUS_MASTER_KEY=devkey-change-me comfynexus:dev

gen-key: ## Print a fresh random master key (base64)
	@openssl rand -base64 48
