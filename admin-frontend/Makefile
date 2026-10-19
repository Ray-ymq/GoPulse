# Component targets for this application.
# The root Makefile dispatches here as: make <target> MODULE=admin-frontend

NPM ?= npm

.PHONY: deps test check build lint

DEVELOPMENT_HELPER := $(MAKE) --no-print-directory -C ../devtools build && ../devtools/bin/devenv

deps:
	@$(DEVELOPMENT_HELPER) frontend-deps --module admin-frontend

test: deps
	$(NPM) test

check: deps
	$(NPM) run typecheck

lint: deps
	$(NPM) run lint

build: deps
	$(NPM) run build
