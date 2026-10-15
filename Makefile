.PHONY: help list-go-modules list-npm-modules doctor bootstrap lint lint-all verify check-compose deps dev dev-observe test race check check-all build build-programs build-images package package-plugin integration e2e stop verify-compose verify-business verify-observe verify-plugins verify-alerts verify-roles verify-pages verify-lifecycle stack-up stack-down stack-verify ci-plan ci-tools ci-workspace ci-check ci-build ci-test check-commits hooks-install

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

# CI and local reproductions use this private workspace so shared Go modules
# come from this checkout, never an older copy downloaded from the module proxy.
CI_WORKSPACE := $(CURDIR)/.run/ci/go.work
LINT_VERSION = $(shell python3 -c 'import json; print(json.load(open("devtools/toolchains.json"))["golangci_lint"]["version"])')
LINT_TOOL = $(CURDIR)/.run/tools/golangci-lint/$(LINT_VERSION)/$(shell go env GOHOSTOS)-$(shell go env GOHOSTARCH)/golangci-lint

# Logical Compose build targets behind the local images. deploy/compose.yaml stays
# the source of truth for each target's Dockerfile context and build arguments.
IMAGE_TARGETS := backend business-worker search-indexer admin-frontend frontend \
                 acceptance router marshaller monitor redis-exporter

REQUIRE_MODULE = $(if $(and $(MODULE),$(filter $(MODULE),$(MODULES))),:,printf '%s\n' '[gopulse] ERROR: specify a registered MODULE, for example MODULE=backend' >&2; exit 2)

help:
	@printf '%s\n' 'GoPulse commands:' '  make deps [SCOPE=]        start the development dependencies for this workspace' '  make dev                  start source Backend, Worker, Indexer, and user Vite' '  make dev-observe          add Router, Marshaller, Monitor, and admin Vite' '  make test MODULE=name     run the component test command' '  make race MODULE=name     run the component race test (Go modules)' '  make check MODULE=name    run the component formatting and static checks' '  make check-all            run make check for every module' '  make build                build every program, frontend, and local image' '  make build MODULE=name    build one component' '  make build-images [CACHE=gha|none] [DRY_RUN=1]  build the local Compose images' '  make package [PLATFORM=] [OUTPUT=] [REGISTRY=] [RUNTIME=1] [PROMOTE=1]' '                            build and verify an immutable release candidate' '  make package-plugin [SOURCE=redis] [VERSION=x.y.z] [ARCH=amd64] [CONTRACT_VERSION=2] [BINARY=path] [OUTPUT=path]' '                            build one official plugin archive' '  make integration [SCOPE=] run native integration checks' '  make e2e [SCOPE=name]     run native business or observability Playwright checks' '  make stop                 stop only this workspace-owned processes and dependencies' '' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)' 'Image targets: $(IMAGE_TARGETS)'
	@printf '%s\n' '  make verify-compose [SCOPE=observability]  run the native Compose acceptance closure' '  make verify-business [SCOPE=v1] [MANIFEST=PATH] [RECEIPT=PATH] [PREFLIGHT=1] [BUDGET=30m] [VERIFY_EVIDENCE=1] [PUBLICATION=PATH]' '                                           run the native business acceptance suite or V1 evidence gate' '  make verify-observe SCOPE=name             run the native observability suite' '  make verify-plugins|verify-alerts|verify-roles|verify-pages  run focused acceptance suites' '  make verify-lifecycle INSTALL=clean|reuse [FAILURE_MATRIX=1] [LIFECYCLE_INSTALL=PATH]  run lifecycle acceptance' '  make stack-up|stack-down|stack-verify       manage the owned Compose stack'
	@printf '%s\n' '' 'Module CI (same commands locally and on GitHub):' '  make ci-plan BASE_REF=ref  show modules changed since the common ancestor' '  make ci-plan ALL=1         show the full module matrix' '  make ci-tools             test CI selection and commit validation' '  make ci-check MODULE=name formatting, static analysis and lint' '  make ci-build MODULE=name build one module' '  make ci-test MODULE=name  unit tests; Go tests include race detection' '  make verify MODULE=name   run ci-check, ci-test and ci-build' '  make verify BASE_REF=ref  run the above checks for affected modules'
	@printf '%s\n' '' 'Commit conventions:' '  make hooks-install        enable commit-msg checks in this repository' '  make check-commits [BASE_REF=ref]  validate new commits since policy adoption' '  make check-commits MESSAGE_FILE=path  validate a commit message file'

list-go-modules:
	@printf '%s\n' $(GO_MODULES)

list-npm-modules:
	@printf '%s\n' $(NPM_MODULES)

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

ci-plan:
	@python3 ci/modules.py plan $(if $(filter 1,$(ALL)),--all,$(if $(BASE_REF),--base-ref "$(BASE_REF)",))

ci-tools:
	@python3 -m unittest discover -s ci -p 'test_*.py' -v

check-commits:
	@python3 ci/commits.py $(if $(MESSAGE_FILE),--message-file "$(MESSAGE_FILE)",$(if $(BASE_REF),--base-ref "$(BASE_REF)",))

hooks-install:
	@configured="$$(git config --get core.hooksPath || true)"; \
	  test -z "$$configured" || test "$$configured" = .githooks || { printf '%s\n' 'Existing core.hooksPath is configured; refusing to overwrite it.' >&2; exit 2; }
	@chmod +x .githooks/commit-msg
	@git config --local core.hooksPath .githooks
	@printf '%s\n' 'Enabled Conventional Commits checks for this repository.'

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
	@$(ACCEPTANCE) business $(if $(SCOPE),--scope $(SCOPE),) $(if $(MANIFEST),--manifest "$(MANIFEST)",) $(if $(RECEIPT),--receipt "$(RECEIPT)",) $(if $(PREFLIGHT),--preflight,) $(if $(BUDGET),--budget "$(BUDGET)",) $(if $(VERIFY_EVIDENCE),--verify-evidence,) $(if $(PUBLICATION),--publication "$(PUBLICATION)",) $(if $(KEEP),--keep,)

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
	@$(ACCEPTANCE) lifecycle --install "$(if $(INSTALL),$(INSTALL),clean)" --manifest "$(if $(MANIFEST),$(MANIFEST),dist/release-manifest.json)" $(if $(LIFECYCLE_INSTALL),--install-path "$(LIFECYCLE_INSTALL)",) $(if $(FAILURE_MATRIX),--failure-matrix,) $(if $(PLATFORM),--platform $(PLATFORM),)

stack-up:
	@$(DEVENV) stack-up

stack-down:
	@$(DEVENV) stack-down

stack-verify:
	@$(DEVENV) stack-verify

