# Shared by Go module Makefiles; the root entry prepares the pinned tool.
GOLANGCI_LINT ?= golangci-lint
.DEFAULT_GOAL := test

.PHONY: lint
lint:
	$(GOLANGCI_LINT) run --allow-serial-runners ./...
