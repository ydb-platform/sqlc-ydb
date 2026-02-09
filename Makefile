# sqlc-ydb: engine plugin + ydb-go-sdk codegen plugin for sqlc (v2 config)
#
# Prerequisites:
#   - Go 1.24+
#   - protoc + protoc-gen-go (for proto). go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
#   - go mod download (so SQLC_MOD_DIR is available for proto)
#   - sqlc with plugin support (for examples), on PATH
#
# Overrides:
#   BINDIR     where to install plugin binaries (default: bin)
#   SQLC       sqlc binary for examples (default: bin/sqlc if build-sqlc was run, else sqlc)
#
# Default: show help. Run 'make build' then 'make build-sqlc' then 'make examples' to generate code.
# build-sqlc builds sqlc from ../engine-plugin (required for plugin support).

REPO_ROOT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
BINDIR    ?= bin
# Prefer bin/sqlc from build-sqlc when present
SQLC      ?= $(firstword $(wildcard $(REPO_ROOT)$(BINDIR)/sqlc) sqlc)
ENGINE_PLUGIN_DIR := $(REPO_ROOT)../engine-plugin

# Use codegen.proto from the sqlc module (via replace => ../engine-plugin or go get).
SQLC_MOD_DIR := $(shell go list -m -f '{{.Dir}}' github.com/sqlc-dev/sqlc)
SQLC_PROTOS  := $(SQLC_MOD_DIR)/protos
PROTO_IN     := $(SQLC_PROTOS)/plugin/codegen.proto
PROTO_OUT    = $(REPO_ROOT)internal/codegen/pb
PBOUT        = $(PROTO_OUT)/codegen.pb.go
ENGINE_BIN   = $(BINDIR)/sqlc-engine-ydb
CODEGEN_BIN  = $(BINDIR)/sqlc-gen-ydb-go-sdk
CODEGEN_DBSQL_BIN = $(BINDIR)/sqlc-gen-ydb-database-sql

# Example dirs under examples/ (each has schema.sql, queries.sql, sqlc.yaml with all plugins).
# Codegen output: examples/<name>/<plugin>/ (e.g. authors/ydb-go-sdk, authors/ydb-database-sql).
# To add an example: create examples/<name>/{schema.sql,queries.sql,sqlc.yaml} and add <name> to EXAMPLES.
EXAMPLES        ?= authors album kv batch booktest jets ondeck
PLUGIN_OUTPUTS  := ydb-go-sdk ydb-database-sql

DOCKER_IMAGE ?= sqlc-ydb

.PHONY: all proto build build-engine build-codegen build-codegen-dbsql build-sqlc examples docker-build clean help

all: help

help:
	@echo "Targets:"
	@echo "  proto         - generate codegen.pb.go from sqlc module's protos/plugin/codegen.proto (needs protoc, protoc-gen-go, go mod download)"
	@echo "  build-engine  - build sqlc-engine-ydb into $(BINDIR)/"
	@echo "  build-codegen - build sqlc-gen-ydb-go-sdk into $(BINDIR)/ (depends on proto)"
	@echo "  build-codegen-dbsql - build sqlc-gen-ydb-database-sql into $(BINDIR)/ (depends on proto)"
	@echo "  build         - proto + build both plugins (ydb-go-sdk + ydb-database-sql)"
	@echo "  build-sqlc    - build sqlc from ../engine-plugin into $(BINDIR)/sqlc (needed for examples)"
	@echo "  examples      - run 'sqlc generate' in each example (EXAMPLES=$(EXAMPLES)); outputs: $(PLUGIN_OUTPUTS)"
	@echo "  docker-build  - build Docker image with sqlc + plugins (DOCKER_IMAGE=$(DOCKER_IMAGE))"
	@echo "  clean         - remove $(BINDIR)/ and generated plugin dirs under examples/"
	@echo ""
	@echo "Overrides: BINDIR=$(BINDIR)  SQLC=$(SQLC)  DOCKER_IMAGE=$(DOCKER_IMAGE)"

# Generate plugin proto Go from the sqlc module's codegen.proto (via go get / replace).
# Output goes to internal/codegen/pb with package pb.
proto: $(PBOUT)
	@echo "ok: $(PBOUT)"

$(PBOUT): $(PROTO_IN)
	@command -v protoc >/dev/null || (echo "error: protoc not found" >&2; exit 1)
	@command -v protoc-gen-go >/dev/null || (echo "error: protoc-gen-go not found (run: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)" >&2; exit 1)
	@mkdir -p $(PROTO_OUT)
	cd $(REPO_ROOT) && protoc -I$(SQLC_PROTOS) \
		--go_out=. --go_opt=module=github.com/sqlc-dev/sqlc-engine-ydb \
		--go_opt=Mplugin/codegen.proto=github.com/sqlc-dev/sqlc-engine-ydb/internal/codegen/pb \
		plugin/codegen.proto
	@if [ ! -f "$(PBOUT)" ]; then \
		echo "error: expected $(PBOUT) to be created; protoc may use different paths" >&2; \
		exit 1; \
	fi
	@echo "ok: $(PBOUT)"

# Build the YDB engine plugin (parses schema + queries).
build-engine: $(BINDIR)
	go build -o $(ENGINE_BIN) ./cmd/sqlc-engine-ydb/
	@echo "ok: $(ENGINE_BIN)"

# Build the ydb-go-sdk codegen plugin. Requires proto.
build-codegen: proto $(BINDIR)
	go build -o $(CODEGEN_BIN) ./cmd/sqlc-gen-ydb-go-sdk/
	@echo "ok: $(CODEGEN_BIN)"

# Build the ydb-database-sql codegen plugin. Requires proto.
build-codegen-dbsql: proto $(BINDIR)
	go build -o $(CODEGEN_DBSQL_BIN) ./cmd/sqlc-gen-ydb-database-sql/
	@echo "ok: $(CODEGEN_DBSQL_BIN)"

# Build both plugins (ydb-go-sdk + ydb-database-sql).
build: build-engine build-codegen build-codegen-dbsql
	@echo "Plugins ready in $(BINDIR)/"

# Build sqlc from engine-plugin into BINDIR (so examples use plugin-aware sqlc).
build-sqlc: $(BINDIR)
	@if [ ! -d "$(ENGINE_PLUGIN_DIR)" ]; then echo "error: $(ENGINE_PLUGIN_DIR) not found (clone engine-plugin next to sqlc-ydb)" >&2; exit 1; fi
	cd $(ENGINE_PLUGIN_DIR) && go build -o $(REPO_ROOT)$(BINDIR)/sqlc ./cmd/sqlc/
	@echo "ok: $(BINDIR)/sqlc"

$(BINDIR):
	mkdir -p $(BINDIR)

# Run sqlc generate in each example dir. One run per example generates all plugin outputs (ydb-go-sdk, ydb-database-sql).
examples: build
	@which $(SQLC) >/dev/null 2>/dev/null || (echo "error: sqlc not found. Run 'make build-sqlc' or set SQLC to a plugin-aware sqlc binary" >&2; exit 1)
	@for ex in $(EXAMPLES); do \
	  echo "sqlc generate in examples/$$ex ..."; \
	  (cd $(REPO_ROOT)examples/$$ex && PATH="$(REPO_ROOT)$(BINDIR):$$PATH" $(SQLC) generate) || exit 1; \
	done
	@echo "ok: examples generated ($(EXAMPLES) -> $(PLUGIN_OUTPUTS))"

# Build Docker image: sqlc (from engine-plugin) + sqlc-engine-ydb + codegen plugins.
# Optional: DOCKER_IMAGE=name, ENGINE_PLUGIN_REF=branch (docker build --build-arg).
docker:
	docker build -t $(DOCKER_IMAGE) .
	@echo "ok: image $(DOCKER_IMAGE)"

clean:
	rm -rf $(REPO_ROOT)$(BINDIR)
	@for ex in $(EXAMPLES); do \
	  for out in $(PLUGIN_OUTPUTS); do \
	    rm -rf "$(REPO_ROOT)examples/$$ex/$$out"; \
	  done; \
	done
	@echo "ok: cleaned"
