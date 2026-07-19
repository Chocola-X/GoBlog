GO ?= go
OUTPUT ?= gopherink
BUILDER_FLAGS ?=

.PHONY: build list-plugins

build:
	$(GO) run ./cmd/gopherink-builder -o "$(OUTPUT)" $(BUILDER_FLAGS)

list-plugins:
	$(GO) run ./cmd/gopherink-builder -list
