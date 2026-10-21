GO ?= go
ARCH ?= amd64
VERSION ?= $(shell cat VERSION)
BINARY := build/gopulse-std-exporter
MODULE_PATH := $(shell $(GO) list -f '{{.Module.Path}}' .)

GOLANGCI_LINT ?= golangci-lint

lint:
	$(GOLANGCI_LINT) run --allow-serial-runners ./...

.PHONY: build linux arm windows test race check package clean
build:
	$(GO) build -trimpath -ldflags '-X $(MODULE_PATH)/common.Version=$(VERSION)' -o $(BINARY) .
linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '-X $(MODULE_PATH)/common.Version=$(VERSION)' -o $(BINARY)-linux-amd64 .
arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '-X $(MODULE_PATH)/common.Version=$(VERSION)' -o $(BINARY)-linux-arm64 .
windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags '-X $(MODULE_PATH)/common.Version=$(VERSION)' -o $(BINARY).exe .
test:
	$(GO) test -count=1 ./...
race:
	$(GO) test -race -count=1 ./...
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	$(GO) vet ./...
package:
	GO=$(GO) python3 package.py --arch $(ARCH) --version $(VERSION)
clean:
	rm -rf build
