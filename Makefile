# Cublyverse developer entry points.
#
# Everything runs through the Go toolchain and pnpm; no extra tools have to be
# installed. Code generators are pinned in go.mod via the `tool` directive.

SHELL := /bin/bash

GO   ?= go
PNPM ?= pnpm

BIN_DIR := bin
BIN     := $(BIN_DIR)/cublyverse
WEB_DIR := web
WEB_STAMP := $(WEB_DIR)/node_modules/.install-stamp

MODULE  := github.com/maksimkurb/cublyverse
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO_LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION)

# Pinned through `tool` directives in go.mod.
OAPI_CODEGEN := $(GO) tool oapi-codegen
SQLC         := $(GO) tool sqlc

.DEFAULT_GOAL := check

.PHONY: all gen gen-go gen-web gen-check build build-web build-go \
        test test-go test-web lint lint-go lint-web typecheck check vet \
        dev migrate clean

all: check

# --- code generation -------------------------------------------------------

gen: gen-go gen-web

gen-go:
	$(OAPI_CODEGEN) -config api/oapi-codegen.yaml api/openapi.yaml
	$(SQLC) generate

gen-web: $(WEB_STAMP)
	cd $(WEB_DIR) && $(PNPM) run gen

# Installs the WebApp dependencies once per lockfile change.
$(WEB_STAMP): $(WEB_DIR)/package.json $(WEB_DIR)/pnpm-lock.yaml $(WEB_DIR)/pnpm-workspace.yaml
	cd $(WEB_DIR) && $(PNPM) install --frozen-lockfile
	@touch $@

# Fails when the generated code does not match api/openapi.yaml, the sqlc
# queries or the migrations.
gen-check:
	@./scripts/check-gen.sh

# --- build -----------------------------------------------------------------

build: build-web build-go

build-web: $(WEB_STAMP)
	cd $(WEB_DIR) && $(PNPM) run build

build-go:
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags "$(GO_LDFLAGS)" -o $(BIN) ./cmd/cublyverse

# --- tests and checks ------------------------------------------------------

test: test-go test-web

test-go:
	$(GO) test ./...

test-web: $(WEB_STAMP)
	cd $(WEB_DIR) && $(PNPM) run test

lint: lint-go lint-web typecheck

lint-go: vet

vet:
	$(GO) vet ./...

lint-web: $(WEB_STAMP)
	cd $(WEB_DIR) && $(PNPM) run lint

typecheck: $(WEB_STAMP)
	cd $(WEB_DIR) && $(PNPM) run typecheck

check: gen-check lint test

# --- run -------------------------------------------------------------------

# Runs the backend with the configuration from .env.
dev: $(WEB_STAMP)
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi; \
	exec $(GO) run ./cmd/cublyverse serve

migrate:
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi; \
	exec $(GO) run ./cmd/cublyverse migrate

clean:
	rm -rf $(BIN_DIR)
	cd $(WEB_DIR) && rm -rf dist && mkdir -p dist && touch dist/.gitkeep
