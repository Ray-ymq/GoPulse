.PHONY: help list-go-modules list-npm-modules list-ci-labels lint-tool lint lint-all test race check check-all build build-programs ci-plan ci-tools ci-workspace ci-check ci-build ci-test check-commits hooks-install version-plan version-update check-versions check-version-files commit

# Component dispatch. This list is the single source of truth for module names;
# each component owns a Makefile that holds its own build and test details.
GO_MODULES := backend/business
NPM_MODULES := frontend/web
MODULES := $(GO_MODULES) $(NPM_MODULES)

GOPULSE_IMAGE_TAG ?= $(shell cat VERSION)
COMPOSE = GOPULSE_IMAGE_TAG="$(GOPULSE_IMAGE_TAG)" docker compose
.DEFAULT_GOAL := help

.PHONY: container-build container-config container-up container-down container-logs

container-build:
	@docker build --build-arg VERSION="$$(cat VERSION)" -t "gopulse/backend:$(GOPULSE_IMAGE_TAG)" -f backend/business/Dockerfile .
	@docker build --build-arg VERSION="$$(cat VERSION)" -t "gopulse/web:$(GOPULSE_IMAGE_TAG)" -f frontend/web/Dockerfile .

container-config:
	@$(COMPOSE) config --quiet

container-up: container-config
	@$(COMPOSE) up -d --wait --wait-timeout 300

container-down:
	@$(COMPOSE) down

container-logs:
	@$(COMPOSE) logs --tail=100 -f

# CI and local reproductions use this private workspace so shared Go modules
# come from this checkout, never an older copy downloaded from the module proxy.
CI_WORKSPACE := $(CURDIR)/.run/ci/go.work
LINT_VERSION = $(shell python3 -c 'import json; print(json.load(open("tools/ci/toolchains.json"))["golangci_lint"]["version"])')
LINT_TOOL = $(CURDIR)/.run/tools/golangci-lint/$(LINT_VERSION)/$(shell go env GOHOSTOS)-$(shell go env GOHOSTARCH)/golangci-lint

REQUIRE_MODULE = $(if $(and $(MODULE),$(filter $(MODULE),$(MODULES))),:,printf '%s\n' '[gopulse] ERROR: specify a registered MODULE, for example MODULE=backend/business' >&2; exit 2)

help:
	@printf '%s\n' 'GoPulse 构建与验证入口：' '  make test / race / check / lint / build MODULE=backend/business' '  make ci-check / ci-test / ci-build MODULE=backend/business' '  make build / ci-tools / list-ci-labels' 'Go modules: $(GO_MODULES)' 'Frontend modules: $(NPM_MODULES)'
	@printf '%s\n' '容器部署入口：' '  make container-build / container-config / container-up / container-down / container-logs' '  GOPULSE_IMAGE_TAG 默认读取 VERSION；部署步骤见 deploy/README.md。'
	@printf '%s\n' '产品版本入口：' '  make version-plan / version-update / commit MESSAGE_FILE=/path/to/message.txt' '  make check-version-files / check-versions；规则见 docs/版本管理.md。'

list-go-modules:
	@printf '%s\n' $(GO_MODULES)

list-npm-modules:
	@printf '%s\n' $(NPM_MODULES)

list-ci-labels:
	@printf '%s\n' $(addprefix ci:,$(GO_MODULES)) ci:none

lint-tool:
	@python3 tools/ci/lint_tool.py

lint:
	@$(REQUIRE_MODULE)
ifneq ($(filter $(MODULE),$(GO_MODULES)),)
	@$(MAKE) --no-print-directory lint-tool
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

ci-plan: export GOPULSE_CI_LABELS := $(LABELS)
ci-plan:
	@test -z "$(ALL)" || { printf '%s\n' 'ALL is no longer supported; select explicit ci:<module> labels.' >&2; exit 2; }
	@test -z "$$GOPULSE_CI_LABELS" || test -z "$(BASE_REF)" || { printf '%s\n' 'LABELS and the local-only BASE_REF mode are mutually exclusive.' >&2; exit 2; }
	@python3 tools/ci/modules.py plan $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

ci-tools:
	@python3 -m unittest discover -s tools/ci -p 'test_*.py' -v

check-commits:
	@python3 tools/ci/commits.py $(if $(MESSAGE_FILE),--message-file "$(MESSAGE_FILE)",$(if $(BASE_REF),--base-ref "$(BASE_REF)",))

version-plan version-update:
	@test -n "$(MESSAGE_FILE)" || { printf '%s\n' '必须指定 MESSAGE_FILE=/path/to/message.txt' >&2; exit 2; }
	@python3 tools/ci/versions.py $(patsubst version-%,%,$@) --message-file "$(MESSAGE_FILE)" $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

commit:
	@test -n "$(MESSAGE_FILE)" || { printf '%s\n' '必须指定 MESSAGE_FILE=/path/to/message.txt' >&2; exit 2; }
	@python3 tools/ci/commits.py --message-file "$(MESSAGE_FILE)"
	@python3 tools/ci/versions.py update --message-file "$(MESSAGE_FILE)" --staged
	@git commit -F "$(MESSAGE_FILE)"

check-versions:
	@python3 tools/ci/versions.py check $(if $(BASE_REF),--base-ref "$(BASE_REF)",)

check-version-files:
	@python3 tools/ci/versions.py check --current-only

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

# 指定 MODULE 时仅构建该模块，否则构建全部模块。
build:
ifdef MODULE
	@$(REQUIRE_MODULE)
	@$(MAKE) --no-print-directory -C $(MODULE) build
else
	@$(MAKE) --no-print-directory build-programs
endif

build-programs:
	@for module in $(MODULES); do \
	  printf '%s\n' "==> build $$module"; \
	  $(MAKE) --no-print-directory -C $$module build || exit 1; \
	done
