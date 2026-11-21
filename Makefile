# splash — developer tasks.
#
# On Windows, make is usually not installed. Every target here is a thin wrapper
# over `node scripts/build.mjs` or a plain go/pnpm command, so all of it works
# without make too; the README lists the underlying commands.

GO      ?= go
NODE    ?= node
PNPM    ?= pnpm
BUILD   := $(NODE) scripts/build.mjs
IMAGE   ?= splash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)

.DEFAULT_GOAL := help

##@ General

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

status: ## Show what the build would produce
	@$(BUILD) status

##@ Develop

install: ## Install Go modules and frontend dependencies
	$(GO) mod download
	cd web && $(PNPM) install --frozen-lockfile

dev: ## Run the Go server and the Vite dev server together
	@$(NODE) scripts/dev.mjs

run: binary ## Build and run the server with the frontend embedded
	@./bin/splash

##@ Build

web: ## Build the frontend and embed it into internal/web/dist
	@$(BUILD) web

binary: ## Build the host binary into bin/
	@$(BUILD) binary

build: web binary ## Build frontend and binary

release: ## Cross-compile every platform into dist/ with archives and checksums
	@$(BUILD) release

docker: ## Build the runtime container image
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

clean: ## Remove build output
	@$(BUILD) clean

##@ Verify

test: test-go test-web ## Run every test

test-go: ## Run Go tests
	$(GO) test ./...

test-web: ## Run frontend tests
	cd web && $(PNPM) test

race: ## Run Go tests under the race detector (needs cgo: Linux or macOS)
	CGO_ENABLED=1 $(GO) test -race ./...

cover: ## Write coverage reports for both halves
	$(GO) test -coverprofile=coverage/go.out -covermode=atomic ./...
	$(GO) tool cover -func=coverage/go.out | tail -1
	cd web && $(PNPM) run test:coverage

lint: lint-go lint-web ## Lint everything

lint-go: ## Check Go formatting and run go vet
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "run 'make fmt'" && exit 1)
	$(GO) vet ./...

lint-web: ## Type-check and lint the frontend
	cd web && $(PNPM) run typecheck
	cd web && $(PNPM) run lint

fmt: ## Format Go and fix frontend lint problems
	gofmt -w .
	cd web && $(PNPM) run lint:fix

typecheck: ## Type-check the frontend without emitting
	cd web && $(PNPM) run typecheck

##@ Verify everything the way CI does

ci: lint test build ## Lint, test and build locally (CI additionally runs -race and a binary smoke test)
