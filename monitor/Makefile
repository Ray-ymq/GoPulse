# Component targets for this Go module.
# The root Makefile dispatches here as: make <target> MODULE=monitor

GO ?= go

# Official plugin archives. The version defaults to the repository VERSION and the
# architecture to the host GOARCH; both can be overridden per invocation, and the
# archive bytes stay reproducible through monitor/cmd/plugin-package.
VERSION ?= $(shell tr -d '[:space:]' < ../VERSION)
SOURCE ?= redis
ARCH ?= $(shell $(GO) env GOARCH)
CONTRACT_VERSION ?= 2

.PHONY: test race check build package-plugin

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

check:
	@test -z "$$(gofmt -l .)" || { printf '%s\n' 'gofmt required for:' >&2; gofmt -l . >&2; exit 1; }
	$(GO) vet ./...

build:
	$(GO) build ./...

package-plugin:
	$(GO) run ./cmd/plugin-package --source $(SOURCE) --version $(VERSION) --arch $(ARCH) --contract-version $(CONTRACT_VERSION) \
	  $(if $(BINARY),--binary $(BINARY)) $(if $(OUTPUT),--output $(OUTPUT))
