.PHONY: build test lint fmt vet check clean install-tools help gate gate-self-clean refactor refactor-campaign install

# Default target
.DEFAULT_GOAL := help

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[1;33m
RED := \033[0;31m
NC := \033[0m # No Color

# Pinned golangci-lint version. Must match the version in
# .github/workflows/ci.yml. Installed via the official script (not
# `go install`) so we get the pre-built binary, which is compiled with
# the latest Go — `go install` would honor the linter's own go.mod
# toolchain and produce a binary built with an older Go that then
# refuses to type-check a project targeting a newer Go.
GOLANGCI_VERSION := v2.12.2

help: ## Show this help message
	@echo "$(BLUE)GoRefactor Build Targets$(NC)"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

install-tools: ## Install required build tools
	@echo "$(BLUE)Installing build tools...$(NC)"
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_VERSION)/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_VERSION)
	go mod download

build: test lint fmt vet ## Build binary (runs all checks first)
	@echo "$(BLUE)Building gorefactor...$(NC)"
	@mkdir -p bin
	go build -o bin/gorefactor ./cmd/gorefactor
	@echo "$(GREEN)✓ Build successful$(NC)"

build-fast: ## Build binary only, no checks (edit-build-retry loop; gate before committing)
	@mkdir -p bin
	go build -o bin/gorefactor ./cmd/gorefactor

install: ## Install binaries + helper scripts globally (on PATH), for use in ANY Go project
	go install ./cmd/gorefactor ./cmd/gorefactor-agent
	@install -m 0755 scripts/gorefactor-delegate.sh "$(shell go env GOPATH)/bin/gorefactor-delegate"
	@install -m 0755 scripts/gorefactor-init-project.sh "$(shell go env GOPATH)/bin/gorefactor-init-project"
	@echo "$(GREEN)✓ installed gorefactor, gorefactor-agent, gorefactor-delegate, gorefactor-init-project → $(shell go env GOPATH)/bin$(NC)"
	@echo "  per project:  cd <proj> && gorefactor-init-project [--write]"

gate: ## Doctor gate: build gorefactor, then lint + build + test (use as a commit/CI gate)
	@mkdir -p bin
	@go build -o bin/gorefactor ./cmd/gorefactor
	@./bin/gorefactor doctor
	@# Ratchet: no new/worsened warning+ structural findings vs the committed
	@# baseline (deterministic fingerprint match; info stays advisory). Skips
	@# quietly when no baseline exists (fresh projects). Re-lock after cleanup
	@# waves with: ./gorefactor lint . --write-baseline
	@test ! -f .gorefactor-lint-baseline.json || ./bin/gorefactor lint . --baseline --fail-on warning --fail-only
	@# One-way enforcement: the baseline itself may only shrink vs HEAD —
	@# re-baselining to absorb regressions fails here. Deliberate growth
	@# (e.g. a new lint rule baselining its backlog):
	@#   BASELINE_GROWTH_OK=1 git commit ...
	@test -n "$(BASELINE_GROWTH_OK)" || ./bin/gorefactor lint . --baseline-ratchet HEAD


# Sunset for non-empty baseline: 2026-10-19. Until then `make gate` allows a
# shrinking baseline; `make gate-self-clean` is the aspirational bar (empty).
BASELINE_SUNSET := 2026-10-19

gate-self-clean: gate ## Fail unless the lint baseline is empty (self-clean release bar)
	@test ! -f .gorefactor-lint-baseline.json && echo "$(GREEN)✓ no baseline (already self-clean)$(NC)" && exit 0; \
	n=$$(python3 -c "import json; print(len(json.load(open('.gorefactor-lint-baseline.json')).get('issues',[])))"); \
	if [ "$$n" -eq 0 ]; then echo "$(GREEN)✓ baseline empty (self-clean)$(NC)"; \
	else echo "$(RED)✗ baseline has $$n entr(y/ies); self-clean bar not met (sunset $(BASELINE_SUNSET))$(NC)"; exit 1; fi

refactor: ## Delegate a spec to gorefactor-agent, Haiku->Sonnet escalation. Usage: make refactor SPEC="..."
	@mkdir -p bin
	@go build -o bin/gorefactor-agent ./cmd/gorefactor-agent
	@scripts/gorefactor-delegate.sh "$(SPEC)" .

refactor-campaign: ## Autonomous, sensor-driven lint cleanup via gorefactor-agent campaign mode
	@mkdir -p bin
	@go build -o bin/gorefactor-agent ./cmd/gorefactor-agent
	@./bin/gorefactor-agent -campaign

test: ## Run all tests
	@echo "$(BLUE)Running tests...$(NC)"
	go test ./... -v -race -coverprofile=coverage.out
	@echo "$(GREEN)✓ Tests passed$(NC)"

coverage: test ## Run tests with coverage report
	@echo "$(BLUE)Generating coverage report...$(NC)"
	go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✓ Coverage report: coverage.html$(NC)"

lint: ## Run golangci-lint
	@echo "$(BLUE)Running linter...$(NC)"
	@command -v golangci-lint >/dev/null 2>&1 || (echo "$(YELLOW)golangci-lint not found. Run: make install-tools$(NC)" && exit 1)
	golangci-lint run ./...
	@echo "$(GREEN)✓ Lint passed$(NC)"

fmt: ## Format code
	@echo "$(BLUE)Formatting code...$(NC)"
	go fmt ./...
	@if [ -x ./bin/gorefactor ]; then ./bin/gorefactor format .; else go run ./cmd/gorefactor format .; fi
	@echo "$(GREEN)✓ Code formatted$(NC)"

vet: ## Run go vet
	@echo "$(BLUE)Running vet...$(NC)"
	go vet ./...
	@echo "$(GREEN)✓ Vet passed$(NC)"

check: ## Run all checks (lint, vet, test, fmt)
	@echo "$(BLUE)Running all checks...$(NC)"
	@$(MAKE) fmt
	@$(MAKE) vet
	@$(MAKE) lint
	@$(MAKE) test
	@echo "$(GREEN)✓ All checks passed$(NC)"

clean: ## Clean build artifacts
	@echo "$(BLUE)Cleaning...$(NC)"
	rm -f gorefactor
	rm -f coverage.out coverage.html
	go clean
	@echo "$(GREEN)✓ Clean complete$(NC)"

# Development helpers
watch-test: ## Watch for changes and run tests
	@echo "$(BLUE)Watching for changes...$(NC)"
	@which watchexec >/dev/null 2>&1 || (echo "$(YELLOW)watchexec not found. Install with: cargo install watchexec-cli$(NC)" && exit 1)
	watchexec -e go "make test"

quick-test: ## Run only failing tests (fast feedback)
	go test ./... -run "TestFind|TestCall|TestInterface" -v

analyze-dir: ## Analyze directory structure (usage: make analyze-dir [DIR=path])
	@echo "$(BLUE)Analyzing codebase...$(NC)"
	./bin/gorefactor lint $(or $(DIR),./analyzer)

find-symbol: ## Find uses of a symbol (usage: make find-symbol SYMBOL=name)
	@if [ -z "$(SYMBOL)" ]; then echo "$(RED)Error: SYMBOL not specified. Usage: make find-symbol SYMBOL=MyFunction$(NC)"; exit 1; fi
	@echo "$(BLUE)Finding uses of '$(SYMBOL)'...$(NC)"
	./bin/gorefactor find-uses $(SYMBOL)

find-callers: ## Find callers of a function (usage: make find-callers FUNC=name)
	@if [ -z "$(FUNC)" ]; then echo "$(RED)Error: FUNC not specified. Usage: make find-callers FUNC=MyFunction$(NC)"; exit 1; fi
	@echo "$(BLUE)Finding callers of '$(FUNC)'...$(NC)"
	./bin/gorefactor find-callers $(FUNC)

# Continuous Integration targets
ci-lint: ## CI: Run linter with strict settings
	golangci-lint run ./... --deadline=5m --max-issues-per-linter=0

ci-test: ## CI: Run tests with coverage
	go test ./... -v -race -coverprofile=coverage.out
	go tool cover -func=coverage.out | grep total | awk '{print "Coverage: " $$3}'

ci-vet: ## CI: Run vet
	go vet ./...

ci: ci-vet ci-lint ci-test ## CI: Run all checks

# Development workflow
dev-setup: install-tools fmt ## Setup development environment
	@echo "$(GREEN)✓ Development environment ready$(NC)"

pre-commit: fmt vet lint ## Run pre-commit checks
	@echo "$(GREEN)✓ Pre-commit checks passed$(NC)"
