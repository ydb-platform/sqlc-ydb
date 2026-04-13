This is experimental support of YDB over engines plugin system (see PR https://github.com/sqlc-dev/sqlc/pull/4247). SQLC external plugin engines system not supported in upstream now. We are waiting review of SQLC maintainer.

# sqlc-ydb

[sqlc](https://sqlc.dev) engine and codegen plugins for [YDB](https://ydb.tech).

- **sqlc-engine-ydb** — engine plugin: implements **`EngineService` / `Parse`** from sqlc’s `protos/engine/engine.proto` (in the **engine-plugin** checkout; same stdin/stdout protobuf contract as other engine plugins; sqlc appends `/engine.EngineService/Parse` as the last argv token). Parses YDB schema and queries.
- **sqlc-gen-ydb-go-sdk** — generates Go code for [ydb-go-sdk](https://github.com/ydb-platform/ydb-go-sdk) (query API, `ParamsBuilder`, `QueryRow`, `Exec`).
- **sqlc-gen-ydb-database-sql** — generates Go code for `database/sql` with YDB driver (DBTX, `ExecContext`, `QueryContext`, `QueryRowContext`).
- **sqlc-gen-ydb-python-sdk** — generates Python code for [ydb-python-sdk](https://github.com/ydb-platform/ydb-python-sdk) (`QuerySessionPool`, `execute_with_retries`, `$name` parameters).

## Engine plugin contract (`sqlc-engine-ydb` and sqlc)

- **Proto:** `service EngineService { rpc Parse (ParseRequest) returns (ParseResponse); }` in sqlc’s `protos/engine/engine.proto` (import path in Go: `github.com/sqlc-dev/sqlc/pkg/engine`).
- **Transport:** protobuf on stdin/stdout (no TCP gRPC). sqlc uses the generated client + `process.Runner`, like codegen plugins.
- **Invocation:** in `sqlc.yaml`, set `engines[].process.cmd` to the executable only (and optional static flags), e.g. `sqlc-engine-ydb`. sqlc **appends** `/engine.EngineService/Parse` when spawning the process — do not add it to `cmd` yourself.
- **Docs:** in the sqlc **engine-plugin** tree: `docs/howto/engine-plugins.md` (entry point) and `docs/guides/engine-plugins.md` (full guide). Published copies may appear under `docs.sqlc.dev` once the engine-plugin docs are released.

This repo’s `go.mod` uses `replace github.com/sqlc-dev/sqlc => ../engine-plugin` so the engine API matches your checkout of **engine-plugin** (or a fork with the same protos).

## Configuration (sqlc.yaml)

Use **version 2** config. Register the engine and the codegen plugins you need; each `codegen` entry produces output in its `out` directory.

### Minimal: one plugin (e.g. ydb-go-sdk)

```yaml
version: "2"

engines:
  - name: ydb
    process:
      cmd: sqlc-engine-ydb

plugins:
  - name: ydb-go-sdk
    process:
      cmd: sqlc-gen-ydb-go-sdk

sql:
  - engine: ydb
    schema: "schema.sql"
    queries: "queries.sql"
    codegen:
      - out: ydb-go-sdk
        plugin: ydb-go-sdk
        options:
          package: db
          # optional, default: github.com/ydb-platform/ydb-go-sdk/v3
          sql_package: github.com/ydb-platform/ydb-go-sdk/v3
```

### database/sql (ydb-database-sql)

```yaml
plugins:
  - name: ydb-database-sql
    process:
      cmd: sqlc-gen-ydb-database-sql

# In sql.codegen add:
      - out: ydb-database-sql
        plugin: ydb-database-sql
        options:
          package: db
```

Output: `ydb-database-sql/models.go`, `db.go`, `queries.sql.go` (Go types, DBTX, retry-wrapped queries).

### ydb-python-sdk

```yaml
plugins:
  - name: ydb-python-sdk
    process:
      cmd: sqlc-gen-ydb-python-sdk

# In sql.codegen add:
      - out: ydb-python-sdk
        plugin: ydb-python-sdk
        options:
          package: db
```

Output: `ydb-python-sdk/models.py`, `queries.py` (or one `.py` per query file), `__init__.py`. Use `Querier(pool)` and call methods like `get_author(id=...)`; add the output dir to `PYTHONPATH` or use as package `db`.

### All plugins in one project

```yaml
version: "2"

engines:
  - name: ydb
    process:
      cmd: sqlc-engine-ydb

plugins:
  - name: ydb-go-sdk
    process:
      cmd: sqlc-gen-ydb-go-sdk
  - name: ydb-database-sql
    process:
      cmd: sqlc-gen-ydb-database-sql
  - name: ydb-python-sdk
    process:
      cmd: sqlc-gen-ydb-python-sdk

sql:
  - engine: ydb
    schema: "schema.sql"
    queries: "queries.sql"
    codegen:
      - out: ydb-go-sdk
        plugin: ydb-go-sdk
        options:
          package: db
      - out: ydb-database-sql
        plugin: ydb-database-sql
        options:
          package: db
      - out: ydb-python-sdk
        plugin: ydb-python-sdk
        options:
          package: db
```

Ensure the plugin binaries (`sqlc-engine-ydb`, `sqlc-gen-ydb-go-sdk`, etc.) are on `PATH` when you run `sqlc generate`.

## Generating code with Docker (recommended)

You don't need to install sqlc or plugins locally. Use the pre-built image (or build it yourself):

```bash
# From your project directory (containing sqlc.yaml, schema.sql, queries.sql)
docker run --rm -v "$(pwd):/src" -w /src ghcr.io/<owner>/sqlc-ydb:latest generate
```

Replace `<owner>` with the GitHub org/user that publishes the image (e.g. `sqlc-dev`). The image includes sqlc (from engine-plugin), **sqlc-engine-ydb**, **sqlc-gen-ydb-go-sdk**, **sqlc-gen-ydb-database-sql**, and **sqlc-gen-ydb-python-sdk**.

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

Generated files appear under each example: `ydb-go-sdk/` and `ydb-database-sql/` (Go: `models.go`, `db.go`, `queries.sql.go`), `ydb-python-sdk/` (Python: `models.py`, `queries.py`, `__init__.py`). `make examples` runs `build-sqlc` and uses **`bin/sqlc`** from **engine-plugin** (required: engine + process plugins). Override with `make examples EXAMPLES_SQLC=/path/to/sqlc` if needed.
