# sqlc-ydb
sqlc-engine-ydb is an external engine of SQLC with support YDB.

## Generating Go code (examples/authors)

The `examples/authors` project uses the v2 config with the **sqlc-engine-ydb** engine plugin and **sqlc-gen-ydb-go-sdk** codegen plugin. To generate Go code:

1. **Build the plugins** (from the sqlc-ydb repo root):
   ```bash
   make build
   ```
2. **Build sqlc from engine-plugin** (requires [engine-plugin](https://github.com/sqlc-dev/sqlc) cloned next to sqlc-ydb, e.g. in `../engine-plugin`):
   ```bash
   make build-sqlc
   ```
3. **Run code generation**:
   ```bash
   make examples
   ```

Generated files appear in `examples/authors/db/` (`models.go`, `db.go`, `queries.sql.go`). The Makefile uses `bin/sqlc` from `make build-sqlc` by default when running `make examples`; you can override with `make examples SQLC=/path/to/sqlc`.
