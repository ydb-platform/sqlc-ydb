# sqlc-ydb

Inspired by [sqlc](https://github.com/sqlc-dev/sqlc), sqlc-ydb brings its SQL-first, typed query workflow to YDB as an independent implementation. Read [the project history](docs/history.md) for the upstream YDB proposals, engine-plugin discussions, and the decision to build a standalone tool.

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE) [![Release](https://img.shields.io/github/v/release/ydb-platform/sqlc-ydb.svg?style=flat-square)](https://github.com/ydb-platform/sqlc-ydb/releases) [![PkgGoDev](https://pkg.go.dev/badge/github.com/ydb-platform/sqlc-ydb/cmd/sqlc-ydb)](https://pkg.go.dev/github.com/ydb-platform/sqlc-ydb/cmd/sqlc-ydb) [![CI](https://github.com/ydb-platform/sqlc-ydb/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ydb-platform/sqlc-ydb/actions/workflows/ci.yml) [![lint](https://github.com/ydb-platform/sqlc-ydb/actions/workflows/lint.yml/badge.svg?branch=main)](https://github.com/ydb-platform/sqlc-ydb/actions/workflows/lint.yml) [![codecov](https://codecov.io/gh/ydb-platform/sqlc-ydb/branch/main/graph/badge.svg?precision=2)](https://app.codecov.io/gh/ydb-platform/sqlc-ydb) ![Code lines](https://sloc.xyz/github/ydb-platform/sqlc-ydb/?category=code) [![View examples](https://img.shields.io/badge/learn-examples-brightgreen.svg)](examples/README.md) [![Telegram](https://img.shields.io/badge/chat-on%20Telegram-2ba2d9.svg)](https://t.me/ydb_en) [![WebSite](https://img.shields.io/badge/website-ydb.tech-blue.svg)](https://ydb.tech) [![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg)](.agents/development.md)

Generate typed query code from YQL for YDB in Go, Python, C++, C#, Java, Kotlin, TypeScript, Rust and PHP. One executable contains the parser, semantic analyzer and generators. Generation works offline and does not require a running YDB, Python, or any separately installed codegen plugin.

Supported YQL and configuration options are listed in the [compatibility contract](docs/compatibility.md). See the [changelog](CHANGELOG.md) and [GitHub Releases](https://github.com/ydb-platform/sqlc-ydb/releases) for release status. Installation and artifact verification are described in [installation](docs/installation.md).

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/ydb-platform/sqlc-ydb/main/install.sh | bash
```

See [installation](docs/installation.md) for pinned versions, release candidates, Windows downloads and PATH setup.

## Feature parity with sqlc

The core `generate`, `compile`, `diff`, `init` and `version` workflow is supported. Configuration is a subset of sqlc v2; queries use YQL parameters and `:one`, `:many`, or `:exec` annotations. Other database engines, external plugins, sqlc macros, database-assisted analysis and cloud workflows are not supported.

See the [first-release summary](CHANGELOG.md) for the comparison and known limitations, and the [compatibility contract](docs/compatibility.md) for the exact CLI, option and YQL coverage.

## Supported targets

| Language | Framework | Single Query | Multiple Queries in One Transaction |
| --- | --- | --- | --- |
| Go | YDB Query SDK | [Test](examples/authors/go/smoke_test.go#L18) | [Test](examples/authors/go/smoke_test.go#L94) |
| Go | database/sql | [Test](examples/authors/go/smoke_test.go#L45) | [Test](examples/authors/go/smoke_test.go#L120) |
| Python | YDB Query SDK | [Test](examples/authors/python/smoke.py#L23) | [Test](examples/authors/python/smoke.py#L36) |
| Python | DB-API | [Test](examples/authors/python/smoke.py#L68) | [Test](examples/authors/python/smoke.py#L69) |
| Python | SQLAlchemy | [Test](examples/authors/python/smoke.py#L82) | [Test](examples/authors/python/smoke.py#L83) |
| C++ | YDB Query SDK | [Test](examples/authors/cpp/native/main.cpp#L100) | [Test](examples/authors/cpp/native/main.cpp#L116) |
| C++ | userver | [Test](examples/authors/cpp/userver/smoke_handler.cpp#L60) | [Test](examples/authors/cpp/userver/smoke_handler.cpp#L80) |
| C# | ADO.NET | [Test](examples/authors/csharp/adonet/Smoke.cs#L14) | [Test](examples/authors/csharp/adonet/Smoke.cs#L34) |
| C# | Dapper | [Test](examples/csharp/Program.cs#L220) | [Test](examples/csharp/Program.cs#L240) |
| Java | YDB Query SDK | [Test](examples/authors/java/native/src/test/java/authors/nativeapi/Smoke.java#L42) | [Test](examples/authors/java/native/src/test/java/authors/nativeapi/Smoke.java#L31) |
| Java | JDBC | [Test](examples/authors/java/jdbc/src/test/java/authors/jdbc/Smoke.java#L39) | [Test](examples/authors/java/jdbc/src/test/java/authors/jdbc/Smoke.java#L27) |
| Java | jOOQ | [Test](examples/java/jooq/src/test/java/LiveTest.java#L73) | [Test](examples/java/jooq/src/test/java/LiveTest.java#L84) |
| Kotlin | YDB Query SDK | [Test](examples/authors/kotlin/src/test/kotlin/authors/smoke/Smoke.kt#L37) | [Test](examples/authors/kotlin/src/test/kotlin/authors/smoke/Smoke.kt#L63) |
| Kotlin | JDBC | [Test](examples/authors/kotlin/src/test/kotlin/authors/smoke/Smoke.kt#L94) | [Test](examples/authors/kotlin/src/test/kotlin/authors/smoke/Smoke.kt#L95) |
| Kotlin | Exposed | [Test](examples/authors/kotlin/src/test/kotlin/authors/smoke/Smoke.kt#L122) | [Test](examples/authors/kotlin/src/test/kotlin/authors/smoke/Smoke.kt#L129) |
| TypeScript | YDB Query SDK | [Test](examples/typescript/smoke.mjs#L34) | [Test](examples/typescript/smoke.mjs#L48) |
| Rust | YDB Query SDK | [Test](examples/rust/tests/live_smoke.rs#L77) | [Test](examples/rust/tests/live_smoke.rs#L94) |
| PHP | YDB SDK | [Test](examples/php/live.php#L63) | — |

Links open integration tests using generated helpers. A single-query example shows an individual helper call; a transaction example shares one transaction across several calls. The caller owns transaction boundaries and retries. These suites run in [CI](.github/workflows/ci.yml) against disposable YDB; locally, live tests require `YDB_CONNECTION_STRING`.

A dash means unsupported. [PHP transaction support](docs/php.md) requires a raw-result API in the SDK that can join an existing transaction.

All targets are built into the executable. Only the generated application needs the selected runtime library. Configuration, generated APIs and type coverage are documented in the [target reference](docs/targets.md).

## Quick start

Build with Go 1.26:

```sh
go build -o bin/sqlc-ydb ./cmd/sqlc-ydb
./bin/sqlc-ydb generate -f examples/authors/sqlc.yaml
./bin/sqlc-ydb compile -f examples/authors/sqlc.yaml
./bin/sqlc-ydb diff -f examples/authors/sqlc.yaml
```

The authors example shares one [schema](examples/authors/schema.sql), [query file](examples/authors/queries.sql) and [configuration](examples/authors/sqlc.yaml) across all targets above.

[All upstream example families](examples/README.md) are also adapted for YDB: authors, batch, booktest, jets and ondeck. Run `make generate` to regenerate them and `make check-examples` to verify analysis, generated outputs, Go builds and Python syntax.

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

Use `sqlc-ydb init` for a starting configuration. Input and output paths are relative to the configuration file. `generate` completes analysis and rendering before writing any files; `compile` writes nothing; `diff` writes nothing and exits with status 1 if generated contents differ. Renamed queries or models can leave obsolete generated files: `generate` and `diff` report these for manual removal in their current output directories. See [output ownership](docs/compatibility.md#output-ownership) when moving outputs or sharing directories between configurations.

## References

- [Compatibility](docs/compatibility.md): supported SQL, configuration and output ownership.
- [Installation](docs/installation.md): source builds, platform archives and version checks.
- [Targets](docs/targets.md): generated APIs, types and runtime contracts.
- [History](docs/history.md): upstream proposals and the standalone project's origins.
- [Source provenance](docs/provenance.md): adapted sources and attribution.

For repository work, start with [AGENTS.md](AGENTS.md).
