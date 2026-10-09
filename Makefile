.PHONY: help deps dev dev-observe test race check check-all build build-programs build-images package package-plugin integration e2e stop verify-compose verify-business verify-observe verify-plugins verify-alerts verify-roles verify-pages verify-lifecycle stack-up stack-down stack-verify

# The development, observation, and stop lifecycles run in the native helper.
# Building it first keeps the failure exit codes and signal handling intact,
# which `go run` cannot preserve.
DEVENV := $(MAKE) --no-print-directory -C devtools build && ./devtools/bin/devenv

# The delivery entry point lives in the lifecycle module because it reuses the
# release manifest contract there. The module builds the binary first: `go run`
# would collapse every non-zero exit status to 1 and lose the tool's contract.
PACKAGE  := $(MAKE) --no-print-directory -C lifecycle package && ./lifecycle/bin/gopulse-package
ACCEPTANCE := $(MAKE) --no-print-directory -C acceptance build && ./acceptance/bin/gopulse-acceptance
PLATFORM ?= linux/amd64
OUTPUT   ?= dist

# Component dispatch. This list is the single source of truth for module names;
# each component owns a Makefile that holds its own build and test details.
GO_MODULES := backend componentmetrics router marshaller monitor lifecycle loadtest \
              exporters/elasticsearch exporters/kafka exporters/mysql \
              exporters/rabbitmq exporters/redis exporters/victoriametrics \
              devtools acceptance
NPM_MODULES := frontend admin-frontend
MODULES := $(GO_MODULES) $(NPM_MODULES)

# Logical Compose build targets behind the local images. deploy/compose.yaml stays
# the source of truth for each target's Dockerfile context and build arguments.
IMAGE_TARGETS := backend business-worker search-indexer admin-frontend frontend \
                 acceptance router marshaller monitor redis-exporter

REQUIRE_MODULE = test -n "$(MODULE)" || { printf '%s\n' '[gopulse] ERROR: MODULE is required, for example make test MODULE=backend' >&2; exit 2; }

help:
	@printf '%s\n' 'GoPulse commands:' '  make deps [SCOPE=]        start the development dependencies for this workspace' '  make dev                  start source Backend, Worker, Indexer, and user Vite' '  make dev-observe          add Router, Marshaller, Monitor, and admin Vite' '  make test MODULE=name     run the component test command' '  make race MODULE=name     run the component race test (Go modules)' '  make check MODULE=name    run the component formatting and static checks' '  make check-all            run make check for every module' '  make build                build every program, frontend, and local image' '  make build MODULE=name    build one component' '  make build-images [CACHE=gha|none] [DRY_RUN=1]  build the local Compose images' '  make package [PLATFORM=] [OUTPUT=] [REGISTRY=] [RUNTIME=1] [PROMOTE=1]' '                            build and verify an immutable release candidate' '  make package-plugin [SOURCE=redis] [VERSION=x.y.z] [ARCH=amd64] [CONTRACT_VERSION=2] [BINARY=path] [OUTPUT=path]' '                            build one official plugin archive' '  make integration [SCOPE=] run native integration checks' '  make e2e [SCOPE=name]     run native business or observability Playwright checks' '  make stop                 stop only this workspace-owned processes and dependencies' '' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)' 'Image targets: $(IMAGE_TARGETS)'
	@printf '%s\n' '  make verify-compose [SCOPE=observability]  run the native Compose acceptance closure' '  make verify-business                       run the native business acceptance suite' '  make verify-observe SCOPE=name             run the native observability suite' '  make verify-plugins|verify-alerts|verify-roles|verify-pages  run focused acceptance suites' '  make verify-lifecycle INSTALL=clean|reuse  run lifecycle acceptance' '  make stack-up|stack-down|stack-verify       manage the owned Compose stack'

test:
	@$(REQUIRE_MODULE)
	@$(MAKE) --no-print-directory -C $(MODULE) test

race:
	@$(REQUIRE_MODULE)
	@$(MAKE) --no-print-directory -C $(MODULE) race

check:
	@$(REQUIRE_MODULE)
	@$(MAKE) --no-print-directory -C $(MODULE) check

check-all:
	@for module in $(MODULES); do \
	  printf '%s\n' "==> $$module"; \
	  $(MAKE) --no-print-directory check MODULE=$$module || exit 1; \
	done

# Bare "make build" builds every program, frontend and local image; passing MODULE
# keeps the per-component build used by callers and by the module jobs.
build:
ifdef MODULE
	@$(MAKE) --no-print-directory -C $(MODULE) build
else
	@$(MAKE) --no-print-directory build-programs
	@$(MAKE) --no-print-directory build-images
endif

build-programs:
	@for module in $(MODULES); do \
	  printf '%s\n' "==> build $$module"; \
	  $(MAKE) --no-print-directory -C $$module build || exit 1; \
	done

CACHE ?= none

# Build definitions and optional GitHub cache policy are owned by devtools so
# local runs and CI invoke the same tested implementation.
build-images:
	@$(DEVENV) build-cache $(if $(filter gha,$(CACHE)),,$(if $(filter none,$(CACHE)),--no-cache,$(error CACHE must be gha or none))) $(if $(DRY_RUN),--print,)

# PLATFORM defaults to the local amd64 candidate. A dual-platform candidate needs
# an emulated builder and runs in CI; PROMOTE=1 additionally requires the
# verification receipts of every platform in the candidate.
package:
	@$(PACKAGE) run --output $(OUTPUT) --platform $(PLATFORM) $(if $(REGISTRY),--registry $(REGISTRY),) $(if $(RUNTIME),--runtime,) $(if $(PROMOTE),--promote,)

package-plugin:
	@$(MAKE) --no-print-directory -C monitor package-plugin

deps:
	@$(DEVENV) deps $(if $(SCOPE),--scope $(SCOPE),)

dev:
	@$(DEVENV) dev

dev-observe:
	@$(DEVENV) dev-observe

integration:
	@$(DEVENV) integration --scope "$(if $(SCOPE),$(SCOPE),business)"

e2e:
	@$(DEVENV) e2e --scope "$(if $(SCOPE),$(SCOPE),business)"

stop:
	@$(DEVENV) stop

verify-compose:
	@$(ACCEPTANCE) compose $(if $(SCOPE),--scope $(SCOPE),) $(if $(KEEP),--keep,)

verify-business:
	@$(ACCEPTANCE) business $(if $(KEEP),--keep,)

verify-observe:
	@$(ACCEPTANCE) observe --scope "$(if $(SCOPE),$(SCOPE),all)" $(if $(KEEP),--keep,)

verify-plugins:
	@$(ACCEPTANCE) plugins $(if $(KEEP),--keep,)

verify-alerts:
	@$(ACCEPTANCE) alerts $(if $(KEEP),--keep,)

verify-roles:
	@$(ACCEPTANCE) roles $(if $(KEEP),--keep,)

verify-pages:
	@$(ACCEPTANCE) pages $(if $(KEEP),--keep,)

verify-lifecycle:
	@$(ACCEPTANCE) lifecycle --install "$(if $(INSTALL),$(INSTALL),clean)" $(if $(MANIFEST),--manifest $(MANIFEST),) $(if $(PLATFORM),--platform $(PLATFORM),)

stack-up:
	@$(DEVENV) stack-up

stack-down:
	@$(DEVENV) stack-down

stack-verify:
	@$(DEVENV) stack-verify

