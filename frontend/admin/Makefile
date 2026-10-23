# Component targets for this application.
# The root Makefile dispatches here as: make <target> MODULE=frontend/admin

NPM ?= npm

.PHONY: deps test check build lint

DEVELOPMENT_HELPER := $(MAKE) --no-print-directory -C ../../devtools build && ../../devtools/bin/devenv

deps:
	@$(DEVELOPMENT_HELPER) frontend-deps --module frontend/admin

test: deps
	$(NPM) test

check: deps
	$(NPM) run typecheck

lint: deps
	$(NPM) run lint

build: deps
	$(NPM) run build
