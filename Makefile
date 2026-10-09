.PHONY: help deps dev dev-observe test race check check-all build build-programs build-images package-plugin integration e2e stop

# The development, observation, and stop lifecycles run in the native helper.
# Building it first keeps the failure exit codes and signal handling intact,
# which `go run` cannot preserve.
DEVENV := $(MAKE) --no-print-directory -C devtools build && ./devtools/bin/devenv

# Component dispatch. This list is the single source of truth for module names;
# each component owns a Makefile that holds its own build and test details.
GO_MODULES := backend componentmetrics router marshaller monitor lifecycle loadtest \
              exporters/elasticsearch exporters/kafka exporters/mysql \
              exporters/rabbitmq exporters/redis exporters/victoriametrics \
              devtools
NPM_MODULES := frontend admin-frontend
MODULES := $(GO_MODULES) $(NPM_MODULES)

# Logical Compose build targets behind the local images. deploy/compose.yaml stays
# the source of truth for each target's Dockerfile context and build arguments.
IMAGE_TARGETS := backend business-worker search-indexer admin-frontend frontend \
                 acceptance router marshaller monitor redis-exporter

REQUIRE_MODULE = test -n "$(MODULE)" || { printf '%s\n' '[gopulse] ERROR: MODULE is required, for example make test MODULE=backend' >&2; exit 2; }

help:
	@printf '%s\n' 'GoPulse commands:' '  make deps [SCOPE=]        start the development dependencies for this workspace' '  make dev                  start source Backend, Worker, Indexer, and user Vite' '  make dev-observe          add Router, Marshaller, Monitor, and admin Vite' '  make test MODULE=name     run the component test command' '  make race MODULE=name     run the component race test (Go modules)' '  make check MODULE=name    run the component formatting and static checks' '  make check-all            run make check for every module' '  make build                build every program, frontend, and local image' '  make build MODULE=name    build one component' '  make build-images [IMAGES=name] [DRY_RUN=1]  build the local Compose images' '  make package-plugin [SOURCE=redis] [VERSION=x.y.z] [ARCH=amd64] [CONTRACT_VERSION=2] [BINARY=path] [OUTPUT=path]' '                            build one official plugin archive' '  make integration [SCOPE=] run native integration checks' '  make e2e [SCOPE=name]     run native business or observability Playwright checks' '  make stop                 stop only this workspace-owned processes and dependencies' '' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)' 'Image targets: $(IMAGE_TARGETS)'

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

# The version metadata mirrors scripts/ci/compose_build_cache.py so the CI cache
# helper and this entry point derive the same build definition from one VERSION.
build-images:
	@set -eu; \
	version="$$(tr -d '[:space:]' < VERSION)"; \
	printf '%s' "$$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$' || { printf '%s\n' "[gopulse] ERROR: VERSION must be major.minor.patch, found '$$version'" >&2; exit 2; }; \
	update="$$(printf '%s' "$$version" | awk -F. '{ printf "%d.%d.%d", $$1, $$2, $$3 + 1 }')"; \
	revision="$$(git rev-parse HEAD 2>/dev/null || printf '%s' unknown)"; \
	targets="$(if $(IMAGES),$(IMAGES),$(IMAGE_TARGETS))"; \
	GOPULSE_VERSION="$$version" GOPULSE_IMAGE_TAG="$$version" GOPULSE_REVISION="$$revision" GOPULSE_UPDATE_VERSION="$$update" \
	  docker compose --env-file .env.example --file deploy/compose.yaml build $(if $(DRY_RUN),--print,) $$targets

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

