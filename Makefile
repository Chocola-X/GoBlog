GO ?= go
OUTPUT ?= gopherink
BUILDER_FLAGS ?=

.PHONY: build list-components list-plugins

build:
	$(GO) run ./cmd/gopherink-builder -o "$(OUTPUT)" $(BUILDER_FLAGS)

list-components:
	$(GO) run ./cmd/gopherink-builder -list

list-plugins: list-components
