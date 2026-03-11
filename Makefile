.PHONY: help all run fmt lint lint-ci vet test check build build-all release-all install clean

help: ## This help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z0-9_-]+:.*?## / {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help

APP_NAME       = opcuactl
APP_SRC        = main.go
ARCHS          = linux/amd64 linux/arm64 linux/arm/v7 darwin/amd64 darwin/arm64
VERSION       := $(shell head -1 version.txt)
COMMIT        := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS       := -ldflags "\
  -X 'github.com/otfabric/opcuactl/cmd/opcuactl.version=$(VERSION)' \
  -X 'github.com/otfabric/opcuactl/cmd/opcuactl.commit=$(COMMIT)' \
  -X 'github.com/otfabric/opcuactl/cmd/opcuactl.buildDate=$(BUILD_DATE)'"
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

check: fmt lint lint-ci vet test ## Run all checks (format, lint, vet, test)

build: check ## Build the application
	@echo "Building $(APP_NAME)"
	@go build $(LDFLAGS) -o $(APP_NAME) $(APP_SRC)

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

install: build ## Install the built binary to /usr/local/bin
	@echo "Installing $(APP_NAME) to /usr/local/bin"
	@sudo install -m 0755 $(APP_NAME) /usr/local/bin/$(APP_NAME)

clean: ## Clean the build artifacts
	@echo "Cleaning build artifacts"
	@rm -f $(APP_NAME)
	@rm -rf $(RELEASE_DIR)
