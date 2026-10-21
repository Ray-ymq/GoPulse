# Component targets for this Go module.
# The root Makefile dispatches here as: make <target> MODULE=exporters/redis

GO ?= go
VERSION ?= $(shell cat VERSION)
ARCH ?= amd64

include ../../make/go-lint.mk

.PHONY: test race check build

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

check:
	@test -z "$$(gofmt -l .)" || { printf '%s\n' 'gofmt required for:' >&2; gofmt -l . >&2; exit 1; }
	$(GO) vet ./...

build:
	$(GO) build -trimpath -ldflags '-X github.com/Ray-ymq/GoPulse/exporters/redis/common.Version=$(VERSION)' -o build/gopulse-redis-exporter .

.PHONY: package
package:
	$(MAKE) -C ../.. package-plugin SOURCE=redis VERSION=$(VERSION) ARCH=$(ARCH)
