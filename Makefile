GOFILES_CMD = if command -v rg >/dev/null 2>&1; then rg --files -0 -g '*.go'; else find . -type f -name '*.go' ! -path './.git/*' -print0; fi

.PHONY: docs-links fmt-check whitespace-check test durable race vet build check

docs-links:
	GOWORK=off go run ./scripts/markdown_links

fmt-check:
	@unformatted="$$( $(GOFILES_CMD) | xargs -0 gofmt -l )"; \
	if [ -n "$$unformatted" ]; then printf '%s\n' "$$unformatted"; exit 1; fi

whitespace-check:
	@if rg -n '[[:blank:]]+$$' --glob '!.git/**' .; then exit 1; fi

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

check: docs-links fmt-check whitespace-check test vet build
	git diff --check
