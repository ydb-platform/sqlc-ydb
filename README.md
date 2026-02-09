# sqlc-ydb

`cmd/sqlc-engine-ydb` is an external engine of SQLC with support YDB.
`cmd/sqlc-gen-ydb-go-sdk` and `cmd/sqlc-gen-ydb-database-sql` are codegen plugins for YDB (ydb-go-sdk and database/sql APIs).

## Generating code with Docker (recommended)

You don't need to install sqlc or plugins locally. Use the pre-built image (or build it yourself):

```bash
# From your project directory (containing sqlc.yaml, schema.sql, queries.sql)
docker run --rm -v "$(pwd):/src" -w /src ghcr.io/<owner>/sqlc-ydb:latest generate
```

Replace `<owner>` with the GitHub org/user that publishes the image (e.g. `sqlc-dev`). The image includes sqlc (from engine-plugin), **sqlc-engine-ydb**, **sqlc-gen-ydb-go-sdk**, and **sqlc-gen-ydb-database-sql**.

To build the image locally (from the sqlc-ydb repo root):

```bash
make docker-build
# Optional: DOCKER_IMAGE=my-sqlc-ydb make docker-build
```

Then run codegen from your project dir:

```bash
docker run --rm -v "$(pwd):/src" -w /src sqlc-ydb generate
```

## Generating code locally

The `examples/authors` project uses the v2 config with the **sqlc-engine-ydb** engine plugin and codegen plugins. To generate Go code on your machine:

1. **Build the plugins** (from the sqlc-ydb repo root):
   ```bash
   make build
   ```
2. **Build sqlc from engine-plugin** (requires [engine-plugin](https://github.com/sqlc-dev/engine-plugin) cloned next to sqlc-ydb, e.g. in `../engine-plugin`):
   ```bash
   make build-sqlc
   ```
3. **Run code generation**:
   ```bash
   make examples
   ```

Generated files appear under each example in `examples/<name>/ydb-go-sdk/` and `examples/<name>/ydb-database-sql/` (`models.go`, `db.go`, `queries.sql.go`). The Makefile uses `bin/sqlc` from `make build-sqlc` by default; override with `make examples SQLC=/path/to/sqlc`.
