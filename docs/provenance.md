# Source provenance

The standalone CLI, model, analyzer and generator renderers are maintained
independently. They do not load sqlc's intermediate AST, compiler or plugin
protocol. Selected YDB algorithms and fixtures have been adapted as described
below; adapted tests live in their owning current suites.

Behavior and API references:

- [sqlc configuration and command documentation](https://docs.sqlc.dev/);
- sqlc's Go generator for familiar query method shapes and defaults;
- sqlc-gen-python checkout `53fa0b2e3d10c4201f7a5a344d00a560330da3bb`
  for dataclass and Querier conventions;
- this repository's previous Apache-licensed engine/plugin code, preserved at
  `da046efe95d7ec65c13cd1f88a9f55804c322f73`, for YDB SDK integration examples.

The parser is an external dependency from `ydb-platform/yql-parsers`, pinned to
[v0.0.1](https://github.com/ydb-platform/yql-parsers/releases/tag/v0.0.1), commit
`60491839bf34d68b65f36dc933312de6abe57bf3` (YQL source
`5d09560f3e988d447a17fdff5b323cb73356bcca`). Dependency license notices remain
in their modules.

The [examples](../examples/README.md) adapt SQL schemas, queries and scenarios
from all five families in `sqlc-dev/sqlc` at commit
[`3c2546a4b47fabbcec3e07df420effb1a464728f`](https://github.com/sqlc-dev/sqlc/tree/3c2546a4b47fabbcec3e07df420effb1a464728f/examples).
PostgreSQL, MySQL and SQLite variants were inspected for distinct scenarios; the
adaptations use YDB SQL and SDKs. Each example README records material changes.
Upstream generated Go, pgx batch wrappers and multi-engine test helpers are not
embedded in this implementation.

YDB-specific adaptations follow the main documentation for
[scalar SELECT](https://ydb.tech/docs/en/yql/reference/syntax/select/?version=main),
[string concatenation](https://ydb.tech/docs/en/yql/reference/syntax/expressions?version=main),
[Yson JSON conversion](https://ydb.tech/docs/en/yql/reference/udf/list/yson?version=main),
and [set operations](https://ydb.tech/docs/en/yql/reference/builtins/dict).
Runtime checks execute the resulting SQL on the local-ydb version pinned in CI.

## Adapted implementation and tests

Historical test scenarios came from
[`ydb-platform/sqlc@8eed5d890396eb03953248a3ec4ab7e28dfaed45`](https://github.com/ydb-platform/sqlc/tree/8eed5d890396eb03953248a3ec4ab7e28dfaed45).
Distinct current behaviors belong to `internal/analyzer` and
`internal/yql/builtins`; no tests load a historical checkout or snapshot corpus.
The following CLI fixtures retain adapted integration coverage:

| Current fixture in `internal/endtoend/testdata` | Historical scenarios |
| --- | --- |
| `expression_types` | `cast_coalesce`, `case_named_params`, `builtins` |
| `aggregate_having` | `having` |
| `union` | `select_union`, `order_by_union` |
| `native_go_types` | `select_text_array`, `types_uuid`, `datatype` |

These fixtures use current configurations, explicit parameter declarations and
supported YQL types. Native Go coverage includes List, optional elements,
Decimal and Uuid. Historical config options and generated outputs are not a
compatibility contract.

The CLI fixture runner borrows discovery and exact generated-file comparison
ideas from `sqlc-dev/sqlc@23e357a414310aa8846e64624da8b8a626b3a610`, specifically
`internal/endtoend/endtoend_test.go` and `internal/endtoend/case_test.go`.
Contributor commands and review rules are in
[development](../.agents/development.md#golden-fixtures).

The converter's grammar paths informed direct-context CASE, CAST, UNION and
grouping analysis. Historical function signatures are an inventory, not a type
oracle: the [built-in resolver](../internal/yql/builtins/README.md) records the
current supported subset and primary YQL references. Unsupported
library/resource functions produce errors until their typing rules are
implemented.

Go parameter binding adapts the historical ParamsBuilder idea against SDK
`v3.151.1`, pinned in [examples/go.mod](../examples/go.mod). The generators also
reference their published SDKs and framework APIs without copying those
implementations. Exact source snapshots and non-obvious API choices are kept in
the maintainer [SDK evidence](../.agents/sdk-evidence.md); public language guides
define the generated API, dependency and runtime ownership contracts.
