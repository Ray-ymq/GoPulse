.PHONY: help dev dev-observe test race check check-all build integration e2e stop monitor-image

PYTHON ?= python3
LOCAL_DEVELOPMENT := $(PYTHON) scripts/ci/local_development.py

# Component dispatch. This list is the single source of truth for module names;
# each component owns a Makefile that holds its own build and test details.
GO_MODULES := backend componentmetrics router marshaller monitor lifecycle loadtest \
              exporters/elasticsearch exporters/kafka exporters/mysql \
              exporters/rabbitmq exporters/redis exporters/victoriametrics
NPM_MODULES := frontend admin-frontend
MODULES := $(GO_MODULES) $(NPM_MODULES)

REQUIRE_MODULE = test -n "$(MODULE)" || { printf '%s\n' '[gopulse] ERROR: MODULE is required, for example make test MODULE=backend' >&2; exit 2; }

help:
	@printf '%s\n' 'GoPulse commands:' '  make dev                  start source Backend, Worker, Indexer, and user Vite' '  make dev-observe          add Router, Marshaller, Monitor, and admin Vite' '  make test MODULE=name     run the component test command' '  make race MODULE=name     run the component race test (Go modules)' '  make check MODULE=name    run the component formatting and static checks' '  make check-all            run make check for every module' '  make build MODULE=name    build the component' '  make integration [SCOPE=] run native integration checks' '  make e2e [SCOPE=name]     run native business or observability Playwright checks' '  make monitor-image        prepare the explicit Linux Monitor development image' '  make stop                 stop only this workspace-owned processes and dependencies' '' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)'

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

build:
	@$(REQUIRE_MODULE)
	@$(MAKE) --no-print-directory -C $(MODULE) build

dev:
	@$(LOCAL_DEVELOPMENT) dev

dev-observe:
	@$(LOCAL_DEVELOPMENT) dev-observe

integration:
	@$(LOCAL_DEVELOPMENT) integration --scope "$(if $(SCOPE),$(SCOPE),business)"

e2e:
	@$(LOCAL_DEVELOPMENT) e2e --scope "$(if $(SCOPE),$(SCOPE),business)"

stop:
	@$(LOCAL_DEVELOPMENT) stop

monitor-image:
	@$(LOCAL_DEVELOPMENT) monitor-image
