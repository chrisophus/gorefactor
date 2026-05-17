.PHONY: build test lint fmt vet check clean install-tools help

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
	go build -o gorefactor ./cmd/gorefactor
	@echo "$(GREEN)✓ Build successful$(NC)"

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
	@if [ -x ./gorefactor ]; then ./gorefactor format .; else go run ./cmd/gorefactor format .; fi
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

analyze-dir: ## Analyze directory structure
	@echo "$(BLUE)Analyzing codebase...$(NC)"
	./refactor-skill.sh analyze-dir ./analyzer
	./refactor-skill.sh find-unused ./analyzer

find-symbol: ## Find uses of a symbol (usage: make find-symbol SYMBOL=name)
	@if [ -z "$(SYMBOL)" ]; then echo "$(RED)Error: SYMBOL not specified. Usage: make find-symbol SYMBOL=MyFunction$(NC)"; exit 1; fi
	@echo "$(BLUE)Finding uses of '$(SYMBOL)'...$(NC)"
	./refactor-skill.sh find-uses $(SYMBOL)

find-callers: ## Find callers of a function (usage: make find-callers FUNC=name)
	@if [ -z "$(FUNC)" ]; then echo "$(RED)Error: FUNC not specified. Usage: make find-callers FUNC=MyFunction$(NC)"; exit 1; fi
	@echo "$(BLUE)Finding callers of '$(FUNC)'...$(NC)"
	./refactor-skill.sh find-callers $(FUNC)

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
