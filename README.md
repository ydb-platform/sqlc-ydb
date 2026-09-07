# sqlc-ydb

Generate typed Go, Python, C++, C#, and Java query code from YQL for YDB. One executable contains
the parser, semantic analyzer, and generators. Generation works offline and does
not require a running YDB, Python, or any separately installed codegen plugin.

This is the first standalone development version, preparing for release 0.0.1.
No release is implied by the version number; see the [changelog](CHANGELOG.md)
and [release plan](docs/release-plan.md). The previous engine-plugin
implementation is preserved in `archive/engine-plugins-2026-09-07`.

## Quick start

Build with Go 1.26:

```sh
go build -o bin/sqlc-ydb ./cmd/sqlc-ydb
./bin/sqlc-ydb generate -f examples/authors/sqlc.yaml
./bin/sqlc-ydb compile -f examples/authors/sqlc.yaml
./bin/sqlc-ydb diff -f examples/authors/sqlc.yaml
```

The example generates Go using the native YDB SDK and `database/sql`; Python
using the native SDK, DB-API, and SQLAlchemy; C++ using the native SDK and userver;
C# using `Ydb.Sdk.Ado`; and Java using the native SDK, JDBC, Spring JDBC, and
Hibernate. The generated application needs the
corresponding runtime library; the generator itself does not.

The example groups generated code and dependencies by language:
`go/database/sql`, `go/native`, `python/dbapi`, `python/sqlalchemy`, and
`python/native`, `cpp/native`, `cpp/userver`, `csharp/adonet`, and
`java/{native,jdbc,spring,hibernate}`. Shared `schema.sql`, `queries.sql`, and `sqlc.yaml` stay in
`examples/authors`.

[All upstream example families](examples/README.md) are also adapted for YDB:
authors, batch, booktest, jets and ondeck. Run `make generate` to regenerate them
and `make check-examples` to verify SQL and generated Go code.

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
Renamed queries or models can leave obsolete generated files: `generate` and
`diff` report these for manual removal in their current output directories.
See [output ownership](docs/compatibility.md#output-ownership) when moving outputs
or sharing directories between configurations.

## Design and compatibility

The familiar sqlc workflow is the compatibility target. This project has its own
implementation and release cycle. It supports only YDB, with built-in generators;
external engine/codegen plugins and their protocols are intentionally excluded.
Plugin configurations require migration to built-in `gen` entries.

The analyzer reads the ANTLR YQL parse tree directly. It resolves names and types
before generators see a query. The shared semantic result describes parameters
and result columns; it is not an intermediate AST. Unsupported constructs and
unresolved types must produce an error rather than an untyped fallback.

See [compatibility](docs/compatibility.md), [targets](docs/targets.md),
[architecture](docs/architecture.md), [compiler roadmap](docs/roadmap.md), and
[development](docs/development.md) for
the implemented scope and remaining work. Target-specific configuration and
examples are described in [C++](docs/cpp.md), [C#](docs/csharp.md), and
[Java](docs/java.md).

The user guide is planned for the SQLC section of ydb.tech near release. The
repository currently contains the quick start, technical references and executable
examples; [the release plan](docs/release-plan.md) tracks site documentation and
consumer acceptance.

For repository work, start with [AGENTS.md](AGENTS.md) and the
[project context](.agents/context.md).
