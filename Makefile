.PHONY: help all run fmt lint lint-ci vet test check build build-all release-all install clean vuln e2e coverage-check coverage-pkg-check fuzz

help: ## This help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

# A parent go.work must not change this module's dependencies.
export GOWORK := off

# Same total as coverage-min in ci.yml.
COVER_MIN := 90
# Per-package floor (TEST-012). main.go only calls os.Exit around Execute, so it
# is not counted. Lowest package when set: cmd/opcuactl, 93.7 %.
COVER_PKG_MIN := 90
FUZZTIME ?= 10s

APP_NAME       = opcuactl
APP_SRC        = main.go
ARCHS          = linux/amd64 linux/arm64 linux/arm/v7 darwin/amd64 darwin/arm64
VERSION       := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
TAG           := $(shell git tag --points-at HEAD 2>/dev/null | head -1)
COMMIT        := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS       := -ldflags "-s -w \
  -X 'main.version=$(VERSION)' \
  -X 'main.tag=$(TAG)' \
  -X 'main.commit=$(COMMIT)' \
  -X 'main.buildDate=$(BUILD_DATE)'"
BIN_DIR       := bin
BIN           := $(BIN_DIR)/$(APP_NAME)
RELEASE_DIR   := release

all: test build build-all ## Test and build the application

run: ## Run the application
	@echo "Running $(APP_NAME)"
	@go run $(APP_SRC)

fmt: ## Format Go code with gofmt
	@echo "Running gofmt"
	@gofmt -w .

lint: ## Run staticcheck
	@echo "Running staticcheck"
	@staticcheck ./...

lint-ci: ## Run golangci-lint (install: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@echo "Running golangci-lint"
	@golangci-lint run ./...

vet: ## Run go vet on project packages
	@echo "Running go vet"
	@go vet ./...

test: ## Run unit tests with race detector
	@echo "Running unit tests (race detector)"
	@go test -count=1 -race ./...

coverage: ## Run tests with coverage on core library only (writes coverage.out)
	@echo "Running coverage"
	@go test -count=1 -race -coverprofile=coverage.out -covermode=atomic ./...

coverage-check: coverage ## Fail below COVER_MIN
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {sub("%","",$$3); print $$3}'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { if (t+0 < m+0) { printf "coverage %.1f%% < %s%%\n", t, m; exit 1 } else printf "coverage %.1f%% >= %s%%\n", t, m }'

coverage-pkg-check: coverage ## Fail when a package is below COVER_PKG_MIN
	@awk -v m="$(COVER_PKG_MIN)" 'NR > 1 && !/\/opcuactl\/main\.go:/ { n[$$1] = $$2; if ($$3 > 0) h[$$1] = 1 } \
	END { for (k in n) { p = k; sub(/\/[^\/]*$$/, "", p); s[p] += n[k]; if (k in h) c[p] += n[k] } \
	for (p in s) { v = 100 * c[p] / s[p]; if (v < m) { printf "%s: %.1f%% < %s%%\n", p, v, m; bad = 1 } } exit bad }' coverage.out

fuzz: ## Run every Fuzz target for FUZZTIME (targets discovered, never listed)
	@for pkg in $$(go list ./...); do \
		for t in $$(go test -list '^Fuzz' $$pkg | grep '^Fuzz'); do \
			echo "fuzz $$pkg $$t"; \
			go test -run '^$$' -fuzz "^$$t$$" -fuzztime $(FUZZTIME) $$pkg || exit 1; \
		done; \
	done

cover: coverage ## Open coverage report in browser
	@echo "Opening coverage report"
	@go tool cover -html=coverage.out

check: fmt lint lint-ci vet vuln test coverage-check coverage-pkg-check ## Run all checks (format, lint, vet, vulnerability scan, test, coverage)

build: check ## Build the application
	@echo "Building $(BIN)"
	@mkdir -p $(BIN_DIR)
	@go build $(LDFLAGS) -o $(BIN) $(APP_SRC)

build-all: ## Build the application for all architectures
	@mkdir -p $(RELEASE_DIR)
	@for arch in $(ARCHS); do \
		os=$${arch%%/*}; \
		rest=$${arch#*/}; \
		cpu=$${rest%%/*}; \
		variant=$${rest#*/}; \
		if [ "$$cpu" = "arm" ] && [ "$$variant" = "v7" ]; then \
			echo "Building $(APP_NAME)-$(VERSION)-$$os-armv7..."; \
			GOOS=$$os GOARCH=$$cpu GOARM=7 go build $(LDFLAGS) -o $(RELEASE_DIR)/$(APP_NAME)-$(VERSION)-$$os-armv7 $(APP_SRC); \
		else \
			echo "Building $(APP_NAME)-$(VERSION)-$$os-$$cpu..."; \
			GOOS=$$os GOARCH=$$cpu go build $(LDFLAGS) -o $(RELEASE_DIR)/$(APP_NAME)-$(VERSION)-$$os-$$cpu $(APP_SRC); \
		fi \
	done

release-all: build-all ## Package the build binaries into tar.gz archives for all architectures
	@mkdir -p $(RELEASE_DIR)
	@for arch in $(ARCHS); do \
		os=$${arch%%/*}; \
		rest=$${arch#*/}; \
		cpu=$${rest%%/*}; \
		variant=$${rest#*/}; \
		if [ "$$cpu" = "arm" ] && [ "$$variant" = "v7" ]; then \
			bin=$(APP_NAME)-$(VERSION)-$$os-armv7; \
		else \
			bin=$(APP_NAME)-$(VERSION)-$$os-$$cpu; \
		fi; \
		echo "Packaging $$bin.tar.gz..."; \
		tar czf $(RELEASE_DIR)/$$bin.tar.gz -C $(RELEASE_DIR) $$bin; \
	done

vuln: ## Scan the module and its dependencies for known vulnerabilities (govulncheck)
	@govulncheck ./...

e2e: ## Build the binary and run the e2e suite against it
	@mkdir -p bin
	@go build $(LDFLAGS) -o bin/opcuactl $(APP_SRC)
	@OPCUACTL_E2E_BIN=$(CURDIR)/bin/opcuactl OPCUACTL_E2E_VERSION=$(VERSION) go test -count=1 -tags=e2e ./e2e/...

install: build ## Install the built binary to /usr/local/bin
	@echo "Installing $(APP_NAME) to /usr/local/bin"
	@sudo install -m 0755 $(BIN) /usr/local/bin/$(APP_NAME)

clean: ## Clean the build artifacts
	@echo "Cleaning build artifacts"
	@rm -rf $(BIN_DIR)
	@rm -rf $(RELEASE_DIR)
