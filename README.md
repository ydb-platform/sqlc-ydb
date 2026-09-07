# sqlc-ydb

Generate typed Go and Python query code from YQL for YDB. One executable contains
the parser, semantic analyzer, and generators. Generation works offline and does
not require a running YDB, Python, or any separately installed codegen plugin.

This is the first standalone development version. The previous engine-plugin
implementation is preserved in `archive/engine-plugins-2026-09-07`.

## Quick start

Build with Go 1.26:

```sh
go build -o bin/sqlc-ydb ./cmd/sqlc-ydb
./bin/sqlc-ydb generate -f examples/authors/sqlc.yaml
./bin/sqlc-ydb compile -f examples/authors/sqlc.yaml
./bin/sqlc-ydb diff -f examples/authors/sqlc.yaml
```

The example generates Go using the native YDB SDK and `database/sql`, and Python
using the native SDK, DB-API, and SQLAlchemy. The generated application needs the
corresponding runtime library; the generator itself does not.

The example groups generated code and dependencies by language:
`go/database/sql`, `go/native`, `python/dbapi`, `python/sqlalchemy`, and
`python/native`. Shared `schema.sql`, `queries.sql`, and `sqlc.yaml` stay in
`examples/authors`.

```yaml
version: "2"
sql:
  - engine: ydb
    schema: schema.sql
    queries: query.sql
    gen:
      go:
        package: db
        out: db
        sql_package: ydb
      python:
        out: queries
        runtime: ydb
```

```sql
-- name: GetAuthor :one
DECLARE $author_id AS Uint64;
SELECT name FROM authors WHERE id = $author_id;
```

Use `sqlc-ydb init` for a starting configuration. Input and output paths are
relative to the configuration file. `generate` completes analysis and rendering
before writing any files; `compile` writes nothing; `diff` writes nothing and
exits with status 1 if generated contents differ.

## Design and compatibility

The familiar sqlc workflow is the compatibility target. This project has its own
implementation and release cycle. It supports only YDB, with built-in generators;
external engine/codegen plugins and their protocols are intentionally excluded.
Plugin configurations require migration to `gen.go` / `gen.python`.

The analyzer reads the ANTLR YQL parse tree directly. It resolves names and types
before generators see a query. The shared semantic result describes parameters
and result columns; it is not an intermediate AST. Unsupported constructs and
unresolved types must produce an error rather than an untyped fallback.

See [compatibility](docs/compatibility.md), [targets](docs/targets.md),
[architecture](docs/architecture.md), and [development](docs/development.md) for
the implemented scope and remaining work. More languages follow after the Go and
Python pipeline is established.
