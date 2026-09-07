# Source provenance

The standalone CLI, model, analyzer and generator renderers were written for
this implementation. They do not import or embed sqlc's intermediate AST,
compiler, plugin protocol, or generator implementation.

Behavior/API references:

- sqlc v1.31.1 configuration and command documentation;
- sqlc's Go generator for familiar query method shapes and defaults;
- sqlc-gen-python checkout `53fa0b2e3d10c4201f7a5a344d00a560330da3bb` for
  dataclass/Querier conventions;
- this repository's previous Apache-licensed engine/plugin code, preserved at
  `da046efe95d7ec65c13cd1f88a9f55804c322f73`, for YDB SDK integration examples.

The parser is an external dependency from `ydb-platform/yql-parsers`, pinned to
`2fbaa5e71a0388828bffc5c6a2c2b0e2cf9680dc` (YQL source
`d9544073fd13d17b30f609fa4fe7b034cd28ba02`). Dependency license notices remain in
their modules. If upstream source code or tests are copied in future changes,
record the source commit and preserve the applicable notices alongside them.
