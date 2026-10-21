ARCH ?= amd64
VERSION := $(shell cat VERSION)
.PHONY: package
package:
	$(MAKE) -C ../.. package-plugin SOURCE=node VERSION=$(VERSION) ARCH=$(ARCH)
