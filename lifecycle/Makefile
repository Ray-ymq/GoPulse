# Component targets for this Go module.
# The root Makefile dispatches here as: make <target> MODULE=lifecycle

GO ?= go
PACKAGE ?= ./bin/gopulse-package

.PHONY: test race check build package

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

check:
	@test -z "$$(gofmt -l .)" || { printf '%s\n' 'gofmt required for:' >&2; gofmt -l . >&2; exit 1; }
	$(GO) vet ./...

build:
	$(GO) build ./...

package:
	$(GO) build -o $(PACKAGE) ./cmd/gopulse-package
