.PHONY: all build test lint clean clean-all tidy deps \
        web-install web-build web-clean web-dev \
        binary build-all build-linux-amd64 build-linux-arm64 build-darwin-arm64 \
        test-v test-cover test-race test-wasm test-executor test-skills test-integration test-count \
        docker docker-run run dev fmt generate check \
        release-snapshot release help

# ============================================================================
# Variables
# ============================================================================
BINARY_NAME := openbotstack
BUILD_DIR := ./build
DOCKER_TAG := openbotstack:latest
GO_FLAGS := -ldflags="-s -w"

# ============================================================================
# Main targets
# ============================================================================

all: web-build build ## Build frontend and Go binary

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ============================================================================
# Go targets
# ============================================================================

# Platforms to build for (os/arch)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

build: ## Build for current platform
	go build ./...

binary: web-build ## Build production binary for current platform
	mkdir -p $(BUILD_DIR)
	go build \
		-ldflags "-s -w \
			-X main.version=$(shell git describe --tags --always 2>/dev/null || echo dev) \
			-X main.commit=$(shell git rev-parse --short HEAD 2>/dev/null || echo none) \
			-X main.branch=$(shell git branch --show-current 2>/dev/null || echo unknown) \
			-X main.buildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" \
		-o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/openbotstack
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

build-all: web-build ## Build binaries for all supported platforms
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/}; \
		OUTPUT_NAME=$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		echo "Building $$OUTPUT_NAME..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH go build $(GO_FLAGS) -o $(BUILD_DIR)/$$OUTPUT_NAME ./cmd/openbotstack; \
	done
	@echo "Build complete. Binaries in $(BUILD_DIR)/"

build-linux-amd64: web-build ## Build for Linux AMD64
	GOOS=linux GOARCH=amd64 go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/openbotstack

build-linux-arm64: web-build ## Build for Linux ARM64
	GOOS=linux GOARCH=arm64 go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/openbotstack

build-darwin-arm64: web-build ## Build for macOS Apple Silicon
	GOOS=darwin GOARCH=arm64 go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/openbotstack

build-darwin-amd64: web-build ## Build for macOS Intel
	GOOS=darwin GOARCH=amd64 go build $(GO_FLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/openbotstack

test: ## Run all tests
	go test ./...

test-v: ## Run all tests with verbose output
	go test -v ./...

test-cover: ## Run tests with coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-race: ## Run tests with race detector
	go test -race ./...

test-wasm: ## Run Wasm runtime tests only
	go test -v ./sandbox/wasm/...

test-wasm-e2e: ## Run Wasm E2E tests (requires compiled skills)
	go test -v -tags=integration -timeout 120s ./sandbox/wasm/ -run "TestE2E"

test-executor: ## Run executor tests only
	go test -v ./executor/...

test-skills: ## Run skill example tests
	go test -v ./examples/skills/...

build-skills: ## Build all skill Wasm modules (requires Go 1.26+)
	@for skill in hello-world tax-calculator wordcount math-add sentiment meeting-summarize; do \
		echo "Building $$skill..." && \
		(cd examples/skills/$$skill && GOOS=wasip1 GOARCH=wasm go build -o main.wasm .) && \
		echo "  OK: examples/skills/$$skill/main.wasm"; \
	done

test-integration: ## Run integration tests (auto-builds binary)
	go test -v -timeout 120s ./integration/...

test-count: ## Count all tests
	@echo "Test counts by package:"
	@go test ./... -v 2>&1 | grep -E "^--- (PASS|FAIL)" | wc -l | xargs echo "Total tests:"

lint: ## Run linter
	golangci-lint run ./...

clean: ## Clean build artifacts and generated files
	go clean ./...
	go clean -testcache
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)
	rm -f coverage.out coverage.html
	rm -f server.log
	rm -rf web/webui/user/dist web/webui/admin/dist

clean-all: clean web-clean ## Clean everything (including node_modules)

tidy: ## Tidy go modules
	go mod tidy

deps: ## Download dependencies
	go mod download

# ============================================================================
# Frontend targets
# ============================================================================

web-install: ## Install frontend dependencies for both UIs
	@set -e; \
	echo "Installing user plane deps..."; \
	cd web/user && npm install; \
	echo "Installing admin plane deps..."; \
	cd ../../web/admin && npm install

web-build: ## Build both frontends for embedding
	@set -e; \
	echo "Building user plane..."; \
	cd web/user && npm run build; \
	echo "Building admin plane..."; \
	cd ../../web/admin && npm run build; \
	echo "User UI built to web/webui/user/dist/"; \
	echo "Admin UI built to web/webui/admin/dist/"

web-dev-user: ## Start user UI dev server
	cd web/user && npm run dev

web-dev-admin: ## Start admin UI dev server
	cd web/admin && npm run dev

web-clean: ## Clean frontend build artifacts
	rm -rf web/user/node_modules web/admin/node_modules
	rm -rf web/webui/user/dist web/webui/admin/dist

# ============================================================================
# Docker targets
# ============================================================================

docker: ## Build Docker image
	docker build -t $(DOCKER_TAG) .

docker-run: ## Run Docker container
	docker run -p 8080:8080 $(DOCKER_TAG)

# ============================================================================
# Development helpers
# ============================================================================

build-dir:
	mkdir -p $(BUILD_DIR)

run: binary ## Build and run locally
	$(BUILD_DIR)/$(BINARY_NAME) --addr=:8080

dev: ## Run with live reload (requires air)
	air

fmt: ## Format Go code
	go fmt ./...
	gofumpt -w .

generate: ## Run go generate
	go generate ./...

check: lint test ## Pre-commit: lint + test

# ============================================================================
# Release targets
# ============================================================================

release-snapshot: ## Build release snapshot (requires goreleaser)
	goreleaser release --snapshot --clean

release: ## Full release (requires goreleaser)
	goreleaser release --clean
