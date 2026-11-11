.PHONY: help list-go-modules list-npm-modules list-ci-labels doctor bootstrap lint lint-all verify check-compose deps dev test race check check-all build build-programs build-images load integration stop stack-up stack-down stack-verify ci-plan ci-tools ci-workspace ci-check ci-build ci-test check-commits hooks-install

# The development, observation, and stop lifecycles run in the native helper.
# Building it first keeps the failure exit codes and signal handling intact,
# which `go run` cannot preserve.
DEVENV := $(MAKE) --no-print-directory -C tools/devtools build && ./tools/devtools/bin/devenv

# Component dispatch. This list is the single source of truth for module names;
# each component owns a Makefile that holds its own build and test details.
GO_MODULES := backend/business \
              backend/runtimekit \
              tools/devtools \
              tools/perflab \
              tools/perflab/mysqlcollector
NPM_MODULES := frontend/user
MODULES := $(GO_MODULES) $(NPM_MODULES)

# CI and local reproductions use this private workspace so shared Go modules
# come from this checkout, never an older copy downloaded from the module proxy.
CI_WORKSPACE := $(CURDIR)/.run/ci/go.work
LINT_VERSION = $(shell python3 -c 'import json; print(json.load(open("tools/devtools/toolchains.json"))["golangci_lint"]["version"])')
LINT_TOOL = $(CURDIR)/.run/tools/golangci-lint/$(LINT_VERSION)/$(shell go env GOHOSTOS)-$(shell go env GOHOSTARCH)/golangci-lint

# Logical Compose build targets behind the local images. deploy/compose/compose.yaml stays
# the source of truth for each target's Dockerfile context and build arguments.
IMAGE_TARGETS := backend business-worker search-indexer frontend

REQUIRE_MODULE = $(if $(and $(MODULE),$(filter $(MODULE),$(MODULES))),:,printf '%s\n' '[gopulse] ERROR: specify a registered MODULE, for example MODULE=backend/business' >&2; exit 2)

help:
	@printf '%s\n' 'GoPulse 社交业务开发入口：' '  make deps / dev / stop' '  make test / race / check / build MODULE=backend/business' '  make build-images' '  make integration SCOPE=business' '  make stack-up / stack-down MODE=business' '  make k8s-up / k8s-down MODE=business' '  make pt' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)'

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
	@docker compose --env-file .env.example --file deploy/compose/compose.yaml --profile operations config --quiet
	@docker compose --env-file .env.example --file deploy/compose/compose.business.yaml --profile load --profile operations config --quiet
	@docker compose --env-file .env.example --file deploy/dev/compose.local.yaml config --quiet
	@docker compose --env-file .env.example --file deploy/compose/compose.yaml --file deploy/dev/compose.debug.yaml config --quiet
	@docker compose --env-file .env.example --file deploy/compose/compose.yaml --file deploy/dev/compose.tracing.yaml config --quiet

ci-plan: export GOPULSE_CI_LABELS := $(LABELS)
ci-plan:
	@test -z "$(ALL)" || { printf '%s\n' 'ALL is no longer supported; select explicit ci:<module> labels.' >&2; exit 2; }
	@test -z "$$GOPULSE_CI_LABELS" || test -z "$(BASE_REF)" || { printf '%s\n' 'LABELS and the local-only BASE_REF mode are mutually exclusive.' >&2; exit 2; }
	@python3 tools/ci/modules.py plan $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

ci-tools:
	@python3 -m unittest discover -s tools/ci -p 'test_*.py' -v

check-commits:
	@python3 tools/ci/commits.py $(if $(MESSAGE_FILE),--message-file "$(MESSAGE_FILE)",$(if $(BASE_REF),--base-ref "$(BASE_REF)",))

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
	@python3 tools/ci/modules.py workspace

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

load:
	@printf '%s\n' '旧 load 入口已停用，请执行 make pt。' >&2
	@exit 2

deps:
	@$(DEVENV) deps $(if $(SCOPE),--scope $(SCOPE),)

dev:
	@$(DEVENV) dev

integration:
	@$(DEVENV) integration --scope "$(if $(SCOPE),$(SCOPE),business)"

stop:
	@$(DEVENV) stop

stack-up:
	@$(DEVENV) stack-up --mode "$(MODE)"

stack-down:
	@$(DEVENV) stack-down --mode "$(MODE)"

stack-verify:
	@$(DEVENV) stack-verify --mode "$(MODE)"


START_RPS ?= 10
.PHONY: pt pt-prepare pt-check pt-status pt-stop pt-report
pt:
	@PYTHONPATH=tools/perflab python3 -m flow run --start-rps "$(START_RPS)"
pt-prepare:
	@PYTHONPATH=tools/perflab python3 -m flow prepare
pt-check:
	@PYTHONPATH=tools/perflab python3 -m flow check
pt-status:
	@PYTHONPATH=tools/perflab python3 -m flow status
pt-stop:
	@PYTHONPATH=tools/perflab python3 -m flow stop
pt-report:
	@PYTHONPATH=tools/perflab python3 -m flow report $(if $(RUN),--run "$(RUN)",)

.PHONY: pt-acceptance
pt-acceptance:
	@PYTHONPATH=tools/perflab python3 -m flow acceptance --start-rps "$(START_RPS)"

# 产品部署方式必须由命令明确指定；源码与压测入口继续使用 Compose。
MODE ?= business
.PHONY: stack-status k8s-install k8s-check k8s-prefetch k8s-up k8s-down k8s-status k8s-access k8s-verify k8s-load-images k8s-render k8s-recreate check-runtime-contracts deployment-tests
stack-status:
	@$(DEVENV) stack-status --mode "$(MODE)"
k8s-install:
	@bash tools/devtools/kubernetes/install.sh
k8s-check:
	@python3 tools/devtools/kubernetes/cluster.py check
k8s-prefetch:
	@python3 tools/devtools/kubernetes/prefetch.py $(if $(filter 1,$(DIRECT)),--direct,)
k8s-up k8s-down k8s-status k8s-access k8s-verify k8s-load-images k8s-render:
	@python3 tools/devtools/kubernetes/k8s.py $(patsubst k8s-%,%,$@) --mode "$(MODE)"
k8s-recreate:
	@python3 tools/devtools/kubernetes/acceptance.py --recreate --mode "$(MODE)"
check-runtime-contracts:
	@python3 tools/devtools/check_contracts.py
deployment-tests:
	@PYTHONPATH=tools/devtools python3 -m unittest discover -s tools/devtools -p 'test_deployment.py' -v
	@PYTHONPATH=tools/devtools/kubernetes python3 -m unittest discover -s tools/devtools/kubernetes -p 'test_*.py' -v

.PHONY: k8s-cluster-verify
k8s-cluster-verify:
	@python3 tools/devtools/kubernetes/cluster.py verify
