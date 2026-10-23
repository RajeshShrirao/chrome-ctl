SHELL := /bin/bash

GO ?= go
NPM ?= npm

BIN_DIR := bin
BIN := $(BIN_DIR)/chrome-ctl
DIST_DIR := dist
EXT_DIR := extension
EXT_DIST := $(EXT_DIR)/dist
NODE_MODULES := $(EXT_DIR)/node_modules
SKILL := SKILL.md
GO_TAGS := -tags nodynamic

.PHONY: all build extension test e2e dist clean help
.DEFAULT_GOAL := build

all: build

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

## build: compile the CLI with cgo disabled
build: | $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build $(GO_TAGS) -trimpath -o $(BIN) ./cmd/chrome-ctl

## extension: install deps when needed, then typecheck and bundle the extension
$(NODE_MODULES): $(EXT_DIR)/package.json $(EXT_DIR)/package-lock.json
	$(NPM) --prefix $(EXT_DIR) install
	@touch $(NODE_MODULES)

extension: $(NODE_MODULES)
	$(NPM) --prefix $(EXT_DIR) run typecheck
	$(NPM) --prefix $(EXT_DIR) run build

## test: Go unit tests plus extension typecheck, build, and tests
test: extension
	$(GO) test $(GO_TAGS) ./...
	$(NPM) --prefix $(EXT_DIR) test

## e2e: build the CLI, then run the end-to-end harness
e2e: build
	bash tests/e2e/run.sh

## dist: release layout with Linux/macOS binaries, extension bundle, and skill
dist: extension
	rm -rf $(DIST_DIR)
	mkdir -p $(DIST_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO) build $(GO_TAGS) -trimpath -o $(DIST_DIR)/chrome-ctl-darwin-arm64 ./cmd/chrome-ctl
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build $(GO_TAGS) -trimpath -o $(DIST_DIR)/chrome-ctl-darwin-amd64 ./cmd/chrome-ctl
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build $(GO_TAGS) -trimpath -o $(DIST_DIR)/chrome-ctl-linux-amd64 ./cmd/chrome-ctl
	cp -R $(EXT_DIST) $(DIST_DIR)/extension
	cp $(SKILL) $(DIST_DIR)/SKILL.md

## clean: remove local CLI and release artifacts (keeps extension builds and deps)
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

## help: list available targets
help:
	@printf '%s\n' \
		'Usage: make <target>' \
		'' \
		'  build      Compile bin/chrome-ctl with CGO_ENABLED=0' \
		'  extension  Install deps if needed, typecheck, and build extension/dist' \
		'  test       Run Go tests and extension typecheck/build/tests' \
		'  e2e        Build the CLI and run the end-to-end harness' \
		'  dist       Cross-compile release binaries and stage extension + SKILL.md' \
		'  clean      Remove bin/ and dist/ (keeps extension/dist and node_modules)' \
		'  help       Show this help'
