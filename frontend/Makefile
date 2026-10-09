# Component targets for this application.
# The root Makefile dispatches here as: make <target> MODULE=frontend

NPM ?= npm

.PHONY: test check build

# Installing only when the directory is absent matches the previous entry point.
node_modules:
	$(NPM) ci --no-audit --no-fund

test: node_modules
	$(NPM) test

check: node_modules
	$(NPM) run typecheck

build: node_modules
	$(NPM) run build
