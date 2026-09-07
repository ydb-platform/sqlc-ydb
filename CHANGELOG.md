# Changelog

Add changes under Unreleased. The [publish workflow](docs/releasing.md) assigns
the version and moves these entries into a numbered section at release time.

## Unreleased

### Added

- Standalone YDB-only CLI with `generate`, `compile`, `diff`, `init`, and
  `version`; `version --verbose` also reports the commit embedded by release builds.
- Direct ANTLR YQL parsing, semantic analysis, and supported schema migration
  operations applied to an in-memory catalog.
- Built-in Go (native SDK, database/sql), Python (native SDK, DB-API, SQLAlchemy),
  C++ (native SDK, userver), C# (ADO.NET), and Java (native SDK, JDBC, Spring JDBC,
  Hibernate) generators.
- Shared authors examples, exact generated-output fixtures, SQL literal
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
