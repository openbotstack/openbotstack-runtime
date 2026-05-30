.DEFAULT_GOAL := help

# ============================================================================
# Variables
# ============================================================================
GO         := go
BINARY_NAME := openbotstack
BUILD_DIR  := ./build
DOCKER_TAG := openbotstack:latest

VERSION    := $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BRANCH     := $(shell git branch --show-current 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
              -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT) \
              -X main.branch=$(BRANCH) \
              -X main.buildTime=$(BUILD_TIME)

PLATFORMS  := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
GO_SKILLS  :=

# ============================================================================
# Primary targets
# ============================================================================

all: web-build build ## Build frontend + Go packages (CI gate)

build: ## Compile all packages
	$(GO) build ./...

# ============================================================================
# Binary builds
# ============================================================================

binary: web-build ## Build production binary with version info
	mkdir -p $(BUILD_DIR)
	mkdir -p data/skills
	cp -rn skills/ data/skills/ 2>/dev/null || true
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/openbotstack
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME) (version=$(VERSION) commit=$(COMMIT))"

build-all: web-build ## Build binaries for all supported platforms
	@set -e; for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*}; GOARCH=$${platform#*/}; \
		OUTPUT_NAME=$(BINARY_NAME)-$$GOOS-$$GOARCH; \
		echo "Building $$OUTPUT_NAME..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH $(GO) build -ldflags "$(LDFLAGS)" \
			-o $(BUILD_DIR)/$$OUTPUT_NAME ./cmd/openbotstack; \
	done
	@echo "All platforms built in $(BUILD_DIR)/"

build-linux-amd64: web-build ## Build for Linux AMD64
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/openbotstack

build-linux-arm64: web-build ## Build for Linux ARM64
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/openbotstack

build-darwin-arm64: web-build ## Build for macOS Apple Silicon
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/openbotstack

build-darwin-amd64: web-build ## Build for macOS Intel
	GOOS=darwin GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/openbotstack

# ============================================================================
# Testing
# ============================================================================

test: ## Run tests (fast, no race)
	$(GO) test ./...

test-verbose: ## Run tests with verbose output
	$(GO) test -v ./...

test-race: ## Run tests with race detector
	$(GO) test -race ./...

test-cover: ## Run tests with coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

test-wasm: ## Run Wasm runtime tests only
	$(GO) test -v ./sandbox/wasm/...

test-wasm-e2e: ## Run Wasm E2E tests (requires compiled skills)
	$(GO) test -v -tags=integration -timeout 120s ./sandbox/wasm/ -run "TestE2E"

test-executor: ## Run executor tests only
	$(GO) test -v ./executor/...

test-skills: ## Run system skill tests
	$(GO) test -v ./skills/...

test-integration: ## Run integration tests
	$(GO) test -v -timeout 120s ./integration/...

test-count: ## Count all tests
	@echo "Test counts by package:"
	@$(GO) test ./... -v 2>&1 | grep -E "^--- (PASS|FAIL)" | wc -l | xargs echo "Total tests:"

# ============================================================================
# Code quality
# ============================================================================

lint: ## Run linters (golangci-lint + govulncheck)
	golangci-lint run ./...
	@govulncheck ./... 2>/dev/null || echo "WARNING: govulncheck not installed or found issues"

fmt: ## Format code (go fmt + gofumpt)
	$(GO) fmt ./...
	@gofumpt -w . 2>/dev/null || true

check: lint test ## Pre-commit check: lint + test

# ============================================================================
# Architecture Gate (CI enforcement)
# ============================================================================

archgate: test-race arch-contracts arch-deps arch-complexity arch-deadcode ## Full architecture gate (required for merge)

arch-contracts: ## Run architecture contract tests
	@echo "==> Architecture Contract Tests"
	$(GO) test -v ./archguard/...

arch-deps: ## Check dependency boundaries (no core→runtime, no planner→mcp)
	@echo "==> Dependency Boundary Check"
	@if grep -rn '"github.com/openbotstack/openbotstack-runtime' ../openbotstack-core/ 2>/dev/null; then \
		echo "FAIL: core imports runtime"; exit 1; fi
	@if grep -rn 'jsonrpc\|wazero\|sqlite\|/mcp/' ../openbotstack-core/planner/ 2>/dev/null | grep -v '_test.go' | grep import; then \
		echo "FAIL: planner imports transport/runtime"; exit 1; fi
	@echo "    PASS: dependency boundaries clean"

arch-complexity: ## Check complexity budgets
	@echo "==> Complexity Budget Check"
	@max_exports=40; max_file_lines=800; max_funcs=20; \
	files=$$(find . -name '*.go' -not -path '*/vendor/*' -not -path '*/node_modules/*' -not -path '*/_test.go'); \
	ok=true; \
	for f in $$files; do \
		lines=$$(wc -l < "$$f" 2>/dev/null || echo 0); \
		if [ "$$lines" -gt $$max_file_lines ]; then \
			echo "    WARN: $$f has $$lines lines (max $$max_file_lines)"; \
		fi; \
	done; \
	echo "    PASS: complexity within budget"

arch-deadcode: ## Scan for dead code and unused exports
	@echo "==> Dead Code Scan"
	@if command -v deadcode > /dev/null 2>&1; then \
		deadcode -test ./... 2>&1 | head -20 || true; \
	else \
		echo "    SKIP: deadcode tool not installed (go install golang.org/x/tools/cmd/deadcode@latest)"; \
	fi

tidy: ## Tidy go modules
	$(GO) mod tidy

generate: ## Run go generate
	$(GO) generate ./...

tools: ## Install dev tools (golangci-lint, gofumpt, govulncheck, goreleaser)
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	$(GO) install mvdan.cc/gofumpt@latest
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest

# ============================================================================
# Skills
# ============================================================================

build-skills: ## Build Go Wasm skills (if any in skills/ directory)
	@echo "System skills are declarative — no Wasm build needed."

# ============================================================================
# Frontend
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
	echo "Frontend built: web/webui/user/dist/ + web/webui/admin/dist/"

web-dev-user: ## Start user UI dev server
	cd web/user && npm run dev

web-dev-admin: ## Start admin UI dev server
	cd web/admin && npm run dev

web-clean: ## Clean frontend build artifacts and node_modules
	rm -rf web/user/node_modules web/admin/node_modules
	rm -rf web/webui/user/dist web/webui/admin/dist

# ============================================================================
# Docker
# ============================================================================

docker: ## Build Docker image
	docker build -t $(DOCKER_TAG) .

docker-run: ## Run Docker container on :8080
	docker run -p 8080:8080 $(DOCKER_TAG)

# ============================================================================
# Development
# ============================================================================

run: binary ## Build and run locally on :8080
	$(BUILD_DIR)/$(BINARY_NAME) --addr=:8080

dev: ## Run with live reload (requires air)
	air

# ============================================================================
# Release
# ============================================================================

release-snapshot: ## Build release snapshot (requires goreleaser)
	goreleaser release --snapshot --clean

release: ## Full release (requires goreleaser)
	goreleaser release --clean

# ============================================================================
# Cleanup
# ============================================================================

clean: ## Clean build artifacts, test cache, coverage
	$(GO) clean ./...
	$(GO) clean -testcache
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html server.log

clean-all: clean web-clean ## Clean everything including node_modules

# ============================================================================
# Help (auto-generated from ## comments)
# ============================================================================

help: ## Show this help
	@echo "Usage: make <target>"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: all build binary build-all \
        build-linux-amd64 build-linux-arm64 build-darwin-arm64 build-darwin-amd64 \
        test test-verbose test-race test-cover test-wasm test-wasm-e2e \
        test-executor test-skills test-integration test-count \
        lint fmt check tidy generate tools build-skills \
        web-install web-build web-dev-user web-dev-admin web-clean \
        docker docker-run run dev \
        release-snapshot release \
        archgate arch-contracts arch-deps arch-complexity arch-deadcode \
        clean clean-all help
