# Component targets for this application.
# The root Makefile dispatches here as: make <target> MODULE=frontend

NPM ?= npm

.PHONY: deps test check build

DEVELOPMENT_HELPER := $(MAKE) --no-print-directory -C ../devtools build && ../devtools/bin/devenv

deps:
	@$(DEVELOPMENT_HELPER) frontend-deps --module frontend

test: deps
	$(NPM) test

check: deps
	$(NPM) run typecheck

build: deps
	$(NPM) run build
