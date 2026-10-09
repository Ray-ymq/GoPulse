# Component targets for this Go module.
# The root Makefile dispatches here as: make <target> MODULE=devtools

GO ?= go

.PHONY: test race check build

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

check:
	@test -z "$$(gofmt -l .)" || { printf '%s\n' 'gofmt required for:' >&2; gofmt -l . >&2; exit 1; }
	$(GO) vet ./...

build:
	@$(GO) build -o bin/devenv ./cmd/devenv
