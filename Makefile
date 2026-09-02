GOFILES_CMD = if command -v rg >/dev/null 2>&1; then rg --files -0 -g '*.go'; else find . -type f -name '*.go' ! -path './.git/*' -print0; fi
SERVICE_VERSION := $(shell tr -d '\r\n' < VERSION)
REVISION ?= $(shell git rev-parse HEAD)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
HOST_GOOS := $(shell go env GOHOSTOS)
HOST_GOARCH := $(shell go env GOHOSTARCH)
ARTIFACT_DIR ?= dist
EXECUTABLE_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)
SIDECAR_NAME := memoryd-$(GOOS)-$(GOARCH)$(EXECUTABLE_SUFFIX)
MEMORYCTL_NAME := memoryctl-$(GOOS)-$(GOARCH)$(EXECUTABLE_SUFFIX)
SIDECAR_CHECKSUM_NAME := $(SIDECAR_NAME).sha256
MEMORYCTL_CHECKSUM_NAME := $(MEMORYCTL_NAME).sha256
MANIFEST_SUPPORT_FLAG ?=

.PHONY: docs-links fmt-check whitespace-check test durable race vet build sidecar sidecar-supported cross-build check corpus-gate m5-benchmark ga-soak release-candidate standalone-preview

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
	GOWORK=off GOOS="$(GOOS)" GOARCH="$(GOARCH)" go build -trimpath \
		-ldflags "-s -w -X github.com/caelis-labs/memory/internal/buildinfo.ServiceVersion=$(SERVICE_VERSION) -X github.com/caelis-labs/memory/internal/buildinfo.BuildRevision=$(REVISION)" \
		-o "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME)" ./cmd/memoryctl
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./cmd/memorymanifest \
		-binary "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" \
		-output "$(ARTIFACT_DIR)/$(SIDECAR_NAME).manifest.json" \
		-service-version "$(SERVICE_VERSION)" -revision "$(REVISION)" \
		-goos "$(GOOS)" -goarch "$(GOARCH)" $(MANIFEST_SUPPORT_FLAG)
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./cmd/memorymanifest \
		-binary "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME)" \
		-output "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME).manifest.json" \
		-service-version "$(SERVICE_VERSION)" -revision "$(REVISION)" \
		-goos "$(GOOS)" -goarch "$(GOARCH)" $(MANIFEST_SUPPORT_FLAG)
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -file "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" -output "$(ARTIFACT_DIR)/$(SIDECAR_CHECKSUM_NAME)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -file "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME)" -output "$(ARTIFACT_DIR)/$(MEMORYCTL_CHECKSUM_NAME)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -verify "$(ARTIFACT_DIR)/$(SIDECAR_CHECKSUM_NAME)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -verify "$(ARTIFACT_DIR)/$(MEMORYCTL_CHECKSUM_NAME)"

sidecar-supported:
	$(MAKE) sidecar MANIFEST_SUPPORT_FLAG=-require-supported

cross-build:
	mkdir -p "$(ARTIFACT_DIR)"
	GOWORK=off GOOS="$(GOOS)" GOARCH="$(GOARCH)" go build -trimpath \
		-ldflags "-s -w -X github.com/caelis-labs/memory/internal/buildinfo.ServiceVersion=$(SERVICE_VERSION) -X github.com/caelis-labs/memory/internal/buildinfo.BuildRevision=$(REVISION)" \
		-o "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" ./cmd/memoryd
	GOWORK=off GOOS="$(GOOS)" GOARCH="$(GOARCH)" go build -trimpath \
		-ldflags "-s -w -X github.com/caelis-labs/memory/internal/buildinfo.ServiceVersion=$(SERVICE_VERSION) -X github.com/caelis-labs/memory/internal/buildinfo.BuildRevision=$(REVISION)" \
		-o "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME)" ./cmd/memoryctl
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./cmd/memorymanifest -binary "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" \
		-output "$(ARTIFACT_DIR)/$(SIDECAR_NAME).manifest.json" -service-version "$(SERVICE_VERSION)" \
		-revision "$(REVISION)" -goos "$(GOOS)" -goarch "$(GOARCH)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./cmd/memorymanifest -binary "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME)" \
		-output "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME).manifest.json" -service-version "$(SERVICE_VERSION)" \
		-revision "$(REVISION)" -goos "$(GOOS)" -goarch "$(GOARCH)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -file "$(ARTIFACT_DIR)/$(SIDECAR_NAME)" -output "$(ARTIFACT_DIR)/$(SIDECAR_CHECKSUM_NAME)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -file "$(ARTIFACT_DIR)/$(MEMORYCTL_NAME)" -output "$(ARTIFACT_DIR)/$(MEMORYCTL_CHECKSUM_NAME)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -verify "$(ARTIFACT_DIR)/$(SIDECAR_CHECKSUM_NAME)"
	GOWORK=off GOOS="$(HOST_GOOS)" GOARCH="$(HOST_GOARCH)" go run ./scripts/artifact_checksum -verify "$(ARTIFACT_DIR)/$(MEMORYCTL_CHECKSUM_NAME)"

m5-benchmark:
	GOWORK=off go test ./internal/appliance -run '^$$' -bench '^BenchmarkM5' -benchtime=200x -benchmem

ga-soak:
	GOWORK=off go run ./scripts/ga_soak -output "$(if $(GA_SOAK_REPORT),$(GA_SOAK_REPORT),dist/ga-soak-report.json)"

release-candidate: check durable race corpus-gate m5-benchmark

standalone-preview: check durable race m5-benchmark sidecar-supported

check: docs-links fmt-check whitespace-check test vet build
	git diff --check

corpus-gate:
	GOWORK=off go test -count=1 -v ./internal/appliance -run '^TestReleaseMultilingualCorpusGate$$'
