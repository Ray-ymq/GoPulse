.PHONY: help list-go-modules list-npm-modules list-ci-labels doctor bootstrap lint lint-all verify check-compose deps dev dev-observe test race check check-all build build-programs build-images package-plugin load integration stop stack-up stack-down stack-verify ci-plan ci-tools ci-workspace ci-check ci-build ci-test check-commits hooks-install

# The development, observation, and stop lifecycles run in the native helper.
# Building it first keeps the failure exit codes and signal handling intact,
# which `go run` cannot preserve.
DEVENV := $(MAKE) --no-print-directory -C devtools build && ./devtools/bin/devenv

# Component dispatch. This list is the single source of truth for module names;
# each component owns a Makefile that holds its own build and test details.
GO_MODULES := backend runtimekit marshaller monitor perflab \
              exporters/elasticsearch exporters/kafka exporters/mysql \
              exporters/rabbitmq exporters/redis exporters/victoriametrics \
              devtools
NPM_MODULES := frontend admin-frontend
MODULES := $(GO_MODULES) $(NPM_MODULES)

# CI and local reproductions use this private workspace so shared Go modules
# come from this checkout, never an older copy downloaded from the module proxy.
CI_WORKSPACE := $(CURDIR)/.run/ci/go.work
LINT_VERSION = $(shell python3 -c 'import json; print(json.load(open("devtools/toolchains.json"))["golangci_lint"]["version"])')
LINT_TOOL = $(CURDIR)/.run/tools/golangci-lint/$(LINT_VERSION)/$(shell go env GOHOSTOS)-$(shell go env GOHOSTARCH)/golangci-lint

# Logical Compose build targets behind the local images. deploy/compose.yaml stays
# the source of truth for each target's Dockerfile context and build arguments.
IMAGE_TARGETS := backend business-worker search-indexer admin-frontend frontend \
                 marshaller monitor

REQUIRE_MODULE = $(if $(and $(MODULE),$(filter $(MODULE),$(MODULES))),:,printf '%s\n' '[gopulse] ERROR: specify a registered MODULE, for example MODULE=backend' >&2; exit 2)

help:
	@printf '%s\n' 'GoPulse commands:' '  make deps [SCOPE=]        start the development dependencies for this workspace' '  make dev                  start source Backend, Worker, Indexer, and user Vite' '  make dev-observe          add Marshaller, Monitor, and admin Vite' '  make test MODULE=name     run the component test command' '  make race MODULE=name     run the component race test (Go modules)' '  make check MODULE=name    run the component formatting and static checks' '  make check-all            run make check for every module' '  make build                build every program, frontend, and local image' '  make build MODULE=name    build one component' '  make build-images [CACHE=gha|none] [DRY_RUN=1]  build the local Compose images' '  make package-plugin [SOURCE=redis] [VERSION=x.y.z] [ARCH=amd64] [CONTRACT_VERSION=2] [BINARY=path] [OUTPUT=path]' '                            build one official plugin archive' '  make load MODE=boundary|refine|soak CONFIG=perflab/k6/config.json' '  make integration [SCOPE=] run native integration checks' '  make stop                 stop only this workspace-owned processes and dependencies' '' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)' 'Image targets: $(IMAGE_TARGETS)'
	@printf '%s\n' '  make stack-up|stack-down|stack-verify       manage the owned Compose stack'
	@printf '%s\n' '' 'Module CI (same commands locally and on GitHub):' '  make list-ci-labels        list supported PR labels' '  make ci-plan LABELS="ci:backend ci:marshaller"  show exactly those module jobs' '  make ci-tools             test CI selection and commit validation' '  make ci-check MODULE=name formatting, static analysis and lint' '  make ci-build MODULE=name build one module' '  make ci-test MODULE=name  unit tests; Go tests include race detection' '  make verify MODULE=name   run ci-check, ci-test and ci-build' '  make verify BASE_REF=ref  local-only checks for directly changed Go modules'
	@printf '%s\n' '' 'Commit conventions:' '  make hooks-install        enable commit-msg checks and the commit template' '  make check-commits [BASE_REF=ref]  validate new commits and PR descriptions in CI' '  make check-commits MESSAGE_FILE=path  validate a commit message file'

list-go-modules:
	@printf '%s\n' $(GO_MODULES)

list-npm-modules:
	@printf '%s\n' $(NPM_MODULES)

list-ci-labels:
	@printf '%s\n' $(addprefix ci:,$(GO_MODULES)) ci:none

doctor:
	@$(DEVENV) doctor

bootstrap:
	@test -z "$(TOOLS_ONLY)" || test -z "$(MODULE)" || { printf '%s\n' '[gopulse] ERROR: TOOLS_ONLY=1 and MODULE are mutually exclusive' >&2; exit 2; }
	@$(DEVENV) bootstrap $(if $(MODULE),--module "$(MODULE)",) $(if $(TOOLS_ONLY),--tools-only,)

lint:
	@$(REQUIRE_MODULE)
ifneq ($(filter $(MODULE),$(GO_MODULES)),)
	@$(MAKE) --no-print-directory bootstrap TOOLS_ONLY=1 MODULE=
	@$(MAKE) --no-print-directory -C $(MODULE) lint GOLANGCI_LINT="$(LINT_TOOL)"
else
	@$(MAKE) --no-print-directory -C $(MODULE) lint
endif

lint-all:
	@status=0; for module in $(MODULES); do \
	  printf '%s\n' "==> lint $$module"; \
	  $(MAKE) --no-print-directory lint MODULE=$$module || status=1; \
	done; exit $$status

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

check-compose:
	@docker compose --env-file .env.example --file deploy/compose.yaml config --quiet

ci-plan: export GOPULSE_CI_LABELS := $(LABELS)
ci-plan:
	@test -z "$(ALL)" || { printf '%s\n' 'ALL is no longer supported; select explicit ci:<module> labels.' >&2; exit 2; }
	@test -z "$$GOPULSE_CI_LABELS" || test -z "$(BASE_REF)" || { printf '%s\n' 'LABELS and the local-only BASE_REF mode are mutually exclusive.' >&2; exit 2; }
	@python3 ci/modules.py plan $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

ci-tools:
	@python3 -m unittest discover -s ci -p 'test_*.py' -v

check-commits:
	@python3 ci/commits.py $(if $(MESSAGE_FILE),--message-file "$(MESSAGE_FILE)",$(if $(BASE_REF),--base-ref "$(BASE_REF)",))

hooks-install:
	@configured="$$(git config --get core.hooksPath || true)"; \
	  test -z "$$configured" || test "$$configured" = .githooks || { printf '%s\n' 'Existing core.hooksPath is configured; refusing to overwrite it.' >&2; exit 2; }
	@configured="$$(git config --get commit.template || true)"; \
	  template="$$(git rev-parse --show-toplevel)/.gitmessage"; \
	  test -z "$$configured" || test "$$configured" = .gitmessage || test "$$configured" = "$$template" || { printf '%s\n' 'Existing commit.template is configured; refusing to overwrite it.' >&2; exit 2; }
	@chmod +x .githooks/commit-msg
	@git config --local core.hooksPath .githooks
	@git config --local commit.template "$$(git rev-parse --show-toplevel)/.gitmessage"
	@printf '%s\n' 'Enabled commit checks and the commit template for this repository.'

ci-workspace:
	@python3 ci/modules.py workspace

ci-check: ci-workspace
	@$(REQUIRE_MODULE)
	@GOWORK="$(CI_WORKSPACE)" $(MAKE) --no-print-directory check MODULE="$(MODULE)"
	@GOWORK="$(CI_WORKSPACE)" $(MAKE) --no-print-directory lint MODULE="$(MODULE)"

ci-build: ci-workspace
	@$(REQUIRE_MODULE)
	@GOWORK="$(CI_WORKSPACE)" $(MAKE) --no-print-directory build MODULE="$(MODULE)"

ci-test: ci-workspace
	@$(REQUIRE_MODULE)
	@GOWORK="$(CI_WORKSPACE)" $(MAKE) --no-print-directory $(if $(filter $(MODULE),$(GO_MODULES)),race,test) MODULE="$(MODULE)"

verify:
ifdef MODULE
	@$(DEVENV) verify --module "$(MODULE)" $(if $(BASE_REF),--base-ref "$(BASE_REF)",)
else ifeq ($(PLAN_ONLY),1)
	@$(MAKE) --no-print-directory ci-plan BASE_REF="$(BASE_REF)"
else
	@$(DEVENV) verify --base-ref "$(BASE_REF)" $(if $(PLAN_ONLY),--plan-only,)
endif

# Bare "make build" builds every program, frontend and local image; passing MODULE
# keeps the per-component build used by callers and by the module jobs.
build:
ifdef MODULE
	@$(REQUIRE_MODULE)
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

package-plugin:
	@$(MAKE) --no-print-directory -C monitor package-plugin

load:
	@$(MAKE) --no-print-directory -C perflab load MODE="$(if $(MODE),$(MODE),boundary)" CONFIG="$(if $(CONFIG),$(CONFIG),perflab/k6/config.json)" $(if $(PREVIOUS_REPORT),PREVIOUS_REPORT="$(PREVIOUS_REPORT)",) $(if $(DRY_RUN),DRY_RUN=1,)

deps:
	@$(DEVENV) deps $(if $(SCOPE),--scope $(SCOPE),)

dev:
	@$(DEVENV) dev

dev-observe:
	@$(DEVENV) dev-observe

integration:
	@$(DEVENV) integration --scope "$(if $(SCOPE),$(SCOPE),business)"

stop:
	@$(DEVENV) stop

stack-up:
	@$(DEVENV) stack-up

stack-down:
	@$(DEVENV) stack-down

stack-verify:
	@$(DEVENV) stack-verify

