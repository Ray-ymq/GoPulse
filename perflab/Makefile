# Component targets for this Go module.
# The root Makefile dispatches here as: make <target> MODULE=perflab

GO ?= go

include ../make/go-lint.mk

.PHONY: test race check build load

test:
	$(GO) test -count=1 ./...
	$(MAKE) flow-test

race:
	$(GO) test -race -count=1 ./...
	$(MAKE) flow-test

check:
	@test -z "$$(gofmt -l .)" || { printf '%s\n' 'gofmt required for:' >&2; gofmt -l . >&2; exit 1; }
	$(GO) vet ./...

build:
	$(GO) build ./...

load:
	@printf '%s\n' '旧 load 入口已停用，请在仓库根目录执行 make pt。' >&2
	@exit 2

.PHONY: flow-test
flow-test:
	PYTHONPATH=. python3 -m unittest discover -s flow -t . -v
	node --test k6/*.test.mjs
	node --input-type=module --check < flow/workload.js
