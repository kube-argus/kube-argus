# kargus — docker build/push for the deployable bundles.
#
#   make build  BUNDLE=operator|service|proxy  REPOSITORY=<registry/org>  VERSION=<tag>
#   make push   BUNDLE=...                      REPOSITORY=...             VERSION=...
#   make release BUNDLE=...                     (build + push)
#
# Omit BUNDLE to act on ALL bundles:
#   make release REPOSITORY=... VERSION=v0.1.0     # builds + pushes all three
#
# Image name is <REPOSITORY>/<BUNDLE>:<VERSION>, e.g.
#   github.com/kube-argus/kube-argus/service:v0.1.0

REPOSITORY ?= github.com/kube-argus/kube-argus
VERSION    ?= latest
BUNDLE     ?=

BUNDLES := operator service proxy
IMG      := $(REPOSITORY)/$(BUNDLE):$(VERSION)

# Each bundle is a --target in the single root Dockerfile. Empty BUNDLE => "all"
# (handled in the recipes); a non-empty invalid BUNDLE errors.
ifneq ($(filter $(BUNDLE),$(BUNDLES) ),)
else ifeq ($(strip $(BUNDLE)),)
else
  $(error BUNDLE must be one of: $(BUNDLES) (got "$(BUNDLE)"))
endif

.PHONY: build push release image help

## build: docker build BUNDLE (or all bundles if BUNDLE is empty)
build:
ifeq ($(strip $(BUNDLE)),)
	@set -e; for b in $(BUNDLES); do $(MAKE) --no-print-directory build BUNDLE=$$b; done
else
	docker build --target $(BUNDLE) -t $(IMG) .
endif

## push: docker push BUNDLE (or all)
push:
ifeq ($(strip $(BUNDLE)),)
	@set -e; for b in $(BUNDLES); do $(MAKE) --no-print-directory push BUNDLE=$$b; done
else
	docker push $(IMG)
endif

## release: build then push BUNDLE (or all)
release:
ifeq ($(strip $(BUNDLE)),)
	@set -e; for b in $(BUNDLES); do $(MAKE) --no-print-directory release BUNDLE=$$b; done
else
	@$(MAKE) --no-print-directory build BUNDLE=$(BUNDLE)
	@$(MAKE) --no-print-directory push  BUNDLE=$(BUNDLE)
endif

## image: print the resolved image ref(s)
image:
ifeq ($(strip $(BUNDLE)),)
	@for b in $(BUNDLES); do echo $(REPOSITORY)/$$b:$(VERSION); done
else
	@echo $(IMG)
endif

## help: list targets
help:
	@echo "Targets: build push release image  (omit BUNDLE to act on all)"
	@echo "Bundles: $(BUNDLES)"
	@echo "Vars:    REPOSITORY=$(REPOSITORY) VERSION=$(VERSION) BUNDLE=$(BUNDLE)"
