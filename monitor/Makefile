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

.PHONY: test race check build package-plugin plugin-package-bin

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
	$(MAKE) --no-print-directory plugin-package-bin
	../.run/bin/plugin-package --source $(SOURCE) --version $(VERSION) --arch $(ARCH) --contract-version $(CONTRACT_VERSION) \
	  $(if $(BINARY),--binary $(BINARY)) $(if $(OUTPUT),--output $(OUTPUT))

# The native acceptance and development callers share one stable executable so
# packaging does not depend on a shell forwarding script or `go run` exit-code
# wrapping. The binary is workspace state and is intentionally ignored.
plugin-package-bin:
	@mkdir -p ../.run/bin
	$(GO) build -trimpath -buildvcs=false -o ../.run/bin/plugin-package ./cmd/plugin-package
