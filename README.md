# sqlc-ydb

Inspired by [sqlc](https://github.com/sqlc-dev/sqlc), sqlc-ydb brings its
SQL-first, typed query workflow to YDB as an independent implementation.
Read [the project history](docs/history.md) for the upstream YDB proposals,
engine-plugin discussions, and the decision to build a standalone tool.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/ydb-platform/sqlc-ydb.svg?style=flat-square)](https://github.com/ydb-platform/sqlc-ydb/releases)
[![CI](https://github.com/ydb-platform/sqlc-ydb/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ydb-platform/sqlc-ydb/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/ydb-platform/sqlc-ydb/branch/main/graph/badge.svg?precision=2)](https://app.codecov.io/gh/ydb-platform/sqlc-ydb)
[![View examples](https://img.shields.io/badge/learn-examples-brightgreen.svg)](examples/README.md)

Generate typed query code from YQL for YDB in Go, Python, C++, C#, Java, Kotlin,
TypeScript, Rust and PHP. One executable contains the parser, semantic analyzer
and generators. Generation works offline and does not require a running YDB,
Python, or any separately installed codegen plugin.

Supported YQL and configuration options are listed in the
[compatibility contract](docs/compatibility.md). See the
[changelog](CHANGELOG.md) and [GitHub Releases](https://github.com/ydb-platform/sqlc-ydb/releases)
for release status. Installation and artifact verification are described in
[installation](docs/installation.md).

## Feature parity with sqlc

| Feature | sqlc-ydb support / difference |
| --- | --- |
| `generate`, `compile`, `diff`, `init`, `version` | Supported. |
| `-f` / `--file`, `-h` / `--help`, `init --v1` / `--v2` | Supported. |
| `--no-remote` / `--remote` | Local execution only; `--no-remote` is accepted, `--remote` is rejected. |
| `completion`, `createdb`, `push`, `verify`, `vet` | Not implemented. |
| Configuration | v2 and basic v1 Go `packages`; generator option coverage is partial. |
| SQL engines | YDB only. |
| Parameters | YQL `$parameter`, with `DECLARE` or supported type inference; no `$1`, `?`, or `@name` compatibility layer. |
| Query annotations | `:one`, `:many`, `:exec`; affected-row counts and other annotations are unsupported. |
| `sqlc.arg`, `sqlc.narg`, `sqlc.embed`, `sqlc.slice` | Not implemented. |
| Generators and plugins | [Built-in targets](docs/targets.md); external plugins are intentionally excluded. |
| Database-assisted analysis | Not implemented. |
| `version --verbose` | Additional option: reports the build commit. |

See the [compatibility contract](docs/compatibility.md) for supported options,
YQL coverage and generated API differences.

## Supported targets

Each link opens the authors example for the selected SDK or framework:

| Language | SDK / framework examples |
| --- | --- |
| Go | [YDB native SDK](examples/authors/go/native), [database/sql](examples/authors/go/database/sql) |
| Python | [YDB native SDK](examples/authors/python/native), [DB-API](examples/authors/python/dbapi), [SQLAlchemy](examples/authors/python/sqlalchemy) |
| C++ | [YDB native SDK](examples/authors/cpp/native), [userver](examples/authors/cpp/userver) |
| C# | [ADO.NET](examples/authors/csharp/adonet), [Dapper](examples/authors/csharp/dapper), [linq2db](examples/authors/csharp/linq2db) |
| Java | [YDB native SDK](examples/authors/java/native), [JDBC](examples/authors/java/jdbc), [Spring JDBC](examples/authors/java/spring), [Hibernate](examples/authors/java/hibernate) |
| Kotlin | [YDB Query SDK, JDBC and Exposed](examples/authors/kotlin) |
| TypeScript | [YDB JavaScript SDK configuration](examples/authors/sqlc.yaml) |
| Rust | [YDB native SDK](examples/authors/rust/native) |
| PHP | [YDB native SDK](examples/authors/php/native) |

All targets are built into the executable. Only the generated application needs
the selected runtime library. Configuration, generated APIs and type coverage
are documented in the [target reference](docs/targets.md).

## Quick start

Build with Go 1.26:

```sh
go build -o bin/sqlc-ydb ./cmd/sqlc-ydb
./bin/sqlc-ydb generate -f examples/authors/sqlc.yaml
./bin/sqlc-ydb compile -f examples/authors/sqlc.yaml
./bin/sqlc-ydb diff -f examples/authors/sqlc.yaml
```

The authors example shares one [schema](examples/authors/schema.sql),
[query file](examples/authors/queries.sql) and
[configuration](examples/authors/sqlc.yaml) across all targets above.

[All upstream example families](examples/README.md) are also adapted for YDB:
authors, batch, booktest, jets and ondeck. Run `make generate` to regenerate them
and `make check-examples` to verify analysis, generated outputs, Go builds and
Python syntax.

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

## References

- [Compatibility](docs/compatibility.md): supported SQL, configuration and output ownership.
- [Installation](docs/installation.md): source builds, platform archives and version checks.
- [Targets](docs/targets.md): generated APIs, types and runtime contracts.
- [History](docs/history.md): upstream proposals and the standalone project's origins.
- [Source provenance](docs/provenance.md): adapted sources and attribution.

For repository work, start with [AGENTS.md](AGENTS.md).
