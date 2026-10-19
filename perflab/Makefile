# Component targets for this Go module.
# The root Makefile dispatches here as: make <target> MODULE=perflab

GO ?= go

include ../make/go-lint.mk

.PHONY: test race check build load

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

check:
	@test -z "$$(gofmt -l .)" || { printf '%s\n' 'gofmt required for:' >&2; gofmt -l . >&2; exit 1; }
	$(GO) vet ./...

build:
	$(GO) build ./...

MODE ?= boundary
CONFIG ?= k6/config.json

load:
	$(GO) run ./cmd/load --mode "$(MODE)" --config "$(CONFIG)" $(if $(PREVIOUS_REPORT),--previous-report "$(PREVIOUS_REPORT)",) $(if $(DRY_RUN),--dry-run,)
