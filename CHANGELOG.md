# Changelog

Add changes under Unreleased. The [publish workflow](docs/releasing.md) assigns
the version and moves these entries into a numbered section at release time.

## Unreleased

### Added

- Standalone YDB-only CLI with `generate`, `compile`, `diff`, `init`, and
  `version`; `version --verbose` also reports the commit embedded by release builds.
- Direct ANTLR YQL parsing, semantic analysis, and supported schema migration
  operations applied to an in-memory catalog.
- CASE/CAST, supported scalar and aggregate functions, UNION/UNION ALL,
  direct-column GROUP BY/HAVING checks and contextual list/LIMIT parameter inference.
- Native Go list parameters, including optional elements and typed empty lists;
  Go Decimal/UUID bindings and native per-query execution options.
- Preserved YDB SQL regression corpus, historical source and license notices
  independent of the retired sqlc fork.
- Built-in Go (native SDK, database/sql), Python (native SDK, DB-API, SQLAlchemy),
  C++ (native SDK, userver), C# (ADO.NET, Dapper, linq2db), Java (native SDK,
  JDBC, Spring JDBC, Hibernate), JavaScript, Rust and PHP generators.
- YDB adaptations of all upstream example families: authors, batch, booktest,
  jets and ondeck, with Go native SDK, database/sql, C# Dapper and linq2db,
  JavaScript, Rust and PHP generation and execution checks.
- Shared authors examples for all built-in languages, exact generated-output fixtures, SQL literal
  round-trip tests, SDK compilation checks, and sequential live-YDB acceptance.
- Release packaging for Linux, macOS, and Windows on amd64 and arm64, with
  SHA256 checksums and version/commit metadata.
- Diagnostics for unsupported expressions, types and options, generated name
  collisions, and obsolete generated files in current output directories.

### Compatibility

- This is an independent implementation of the sqlc workflow. Only YDB and
  built-in generators are supported; external engine/codegen plugins are excluded.
- The initial version implements a documented subset of YQL and sqlc options,
  not every upstream recipe. See [compatibility](docs/compatibility.md).
