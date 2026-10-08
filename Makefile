.PHONY: help dev dev-observe test integration e2e stop monitor-image

PYTHON ?= python3
LOCAL_DEVELOPMENT := $(PYTHON) scripts/ci/local_development.py

help:
	@printf '%s\n' 'GoPulse local commands:' '  make dev              start source Backend, Worker, Indexer, and user Vite' '  make dev-observe      add Router, Marshaller, Monitor, and admin Vite' '  make test MODULE=name run the smallest module test command' '  make e2e [SCOPE=name] run native business or observability Playwright checks' '  make monitor-image    prepare the explicit Linux Monitor development image' '  make stop             stop only this workspace-owned processes and dependencies'

dev:
	@$(LOCAL_DEVELOPMENT) dev

dev-observe:
	@$(LOCAL_DEVELOPMENT) dev-observe

test:
	@test -n "$(MODULE)" || { printf '%s\n' '[gopulse] ERROR: MODULE is required, for example make test MODULE=backend' >&2; exit 2; }
	@$(LOCAL_DEVELOPMENT) test --module "$(MODULE)"

integration:
	@$(LOCAL_DEVELOPMENT) integration --scope "$(if $(SCOPE),$(SCOPE),business)"

e2e:
	@$(LOCAL_DEVELOPMENT) e2e --scope "$(if $(SCOPE),$(SCOPE),business)"

stop:
	@$(LOCAL_DEVELOPMENT) stop

monitor-image:
	@$(LOCAL_DEVELOPMENT) monitor-image
