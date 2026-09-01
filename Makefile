GOFILES_CMD = if command -v rg >/dev/null 2>&1; then rg --files -0 -g '*.go'; else find . -type f -name '*.go' ! -path './.git/*' -print0; fi
SERVICE_VERSION := $(shell tr -d '\r\n' < VERSION)
REVISION ?= $(shell git rev-parse HEAD)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
ARTIFACT_DIR ?= dist
SIDECAR_NAME := memoryd-$(GOOS)-$(GOARCH)
MANIFEST_SUPPORT_FLAG ?=

.PHONY: docs-links fmt-check whitespace-check test durable race vet build sidecar sidecar-supported check m5-benchmark release-candidate

docs-links:
	GOWORK=off go run ./scripts/markdown_links

fmt-check:
	@unformatted="$$( $(GOFILES_CMD) | xargs -0 gofmt -l )"; \
	if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi

whitespace-check:
	@if git grep --untracked -n -E '[[:blank:]]+$$' -- .; then exit 1; fi

test:
	GOWORK=off go test ./...

durable:
	GOWORK=off go test -count=1 ./internal/systemtest

race:
	GOWORK=off go test -race ./...

vet:
	GOWORK=off go vet ./...

build:
	GOWORK=off go build ./cmd/...

sidecar:
	@test -z "$$(git status --short)" || { echo "sidecar packaging requires a clean worktree"; exit 1; }
	@test "$(REVISION)" = "$$(git rev-parse HEAD)" || { echo "REVISION must equal the exact checked-out HEAD"; exit 1; }
	mkdir -p "$(ARTIFACT_DIR)"
	GOWORK=off GOOS="$(GOOS)" GOARCH="$(GOARCH)" go build -trimpath \
		-ldflags "-s -w -X github.com/caelis-labs/memory/internal/buildinfo.ServiceVersion=$(SERVICE_VERSION) -X github.com/caelis-labs/memory/internal/buildinfo.BuildRevision=$(REVISION)" \
		-o "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" ./cmd/memoryd
	GOWORK=off go run ./cmd/memorymanifest \
		-binary "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" \
		-output "$(ARTIFACT_DIR)/$(SIDECAR_NAME).manifest.json" \
		-service-version "$(SERVICE_VERSION)" -revision "$(REVISION)" \
		-goos "$(GOOS)" -goarch "$(GOARCH)" $(MANIFEST_SUPPORT_FLAG)

sidecar-supported:
	$(MAKE) sidecar MANIFEST_SUPPORT_FLAG=-require-supported

m5-benchmark:
	GOWORK=off go test ./internal/appliance -run '^$$' -bench '^BenchmarkM5' -benchtime=50x -benchmem

release-candidate: check durable race m5-benchmark sidecar-supported

check: docs-links fmt-check whitespace-check test vet build
	git diff --check
