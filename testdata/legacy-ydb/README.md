# Preserved YDB work from the sqlc fork

This directory makes the useful YDB work independent of the retired
`ydb-platform/sqlc` repository. It preserves commit
`8eed5d890396eb03953248a3ec4ab7e28dfaed45`, relative to upstream sqlc commit
`2e0435c856c7d42ea58aaa2c24b6c9feda0509e9`. The source MIT notice is in
[LICENSE](LICENSE).

## Contents

- [manifest.json](manifest.json) records source paths, revision, SHA256 checksums
  and the mapping from 181 original configurations to 131 distinct combinations
  of schema/query inputs across 126 scenario names.
- `inputs/` preserves the original SQL and configurations byte for byte,
  including driver variants. Configuration options and generated files from the
  old implementation are not treated as the current compatibility contract.
- [ydb.patch](ydb.patch) preserves the complete YDB branch contribution: 1,208
  changed files, including all function catalogs, the ANTLR converter, Go
  generator, generated expectations and examples. It is a readable Git patch
  with full object IDs and binary payloads. It is historical reference material;
  none of the old AST, compiler or plugin implementation is compiled or loaded.
- `expected/` records the current analyzer result for every distinct input:
  resolved catalog/parameters/result columns, or exact source diagnostics.
  Rejected cases remain visible coverage gaps or invalid legacy inputs; they
  are neither skipped nor counted as implemented YQL support.

The earlier inventory of 123 scenarios and 177 configurations omitted three
YDB scenarios outside the usual `ydb/` directory and one YAML configuration.
The manifest includes these inputs too.

## Executable tests and adaptations

`internal/endtoend/legacy_test.go` checks every preserved file hash and runs the
whole SQL corpus through the current analyzer without Git, network access, an
SDK, or a database. Identical driver inputs share one semantic assertion.
For the legacy `datatype` directory only, query files are excluded from the
schema file list even though the original config selected their shared directory.
No SQL bytes are rewritten.

Run from the repository root:

```sh
go test ./internal/endtoend -run 'TestLegacy(SourceIntegrity|Corpus)$'
# Only after reviewing a deliberate semantic change:
go test ./internal/endtoend -run TestLegacyCorpus -update-legacy
```

Updating refuses an accepted-to-rejected transition. Review changed semantic
expectations against the YQL contract; the old generated Go files are not a
type-correctness oracle. Keep original inputs immutable and place adaptations
in ordinary CLI fixtures. The initial adaptations are:

- [CASE, CAST and scalar functions](../../internal/endtoend/testdata/legacy_expression_types)
- [grouped aggregates and HAVING](../../internal/endtoend/testdata/legacy_aggregate_having)
- [UNION and UNION ALL](../../internal/endtoend/testdata/legacy_union)
- [native Go lists, optional elements, Decimal and Uuid](../../internal/endtoend/testdata/legacy_native_go_types)

Each adaptation records its original scenarios and changes, including replacing
inherited PostgreSQL types such as `Serial` with actual YQL types and making
parameter declarations explicit. Macros, subqueries and other unsupported
constructs remain explicit errors until implemented with their own tests.

## Historical implementation references

Search `ydb.patch` for these original paths:

| Original path | Preserved ideas |
| --- | --- |
| `internal/engine/ydb/lib/` | Ordinary, aggregate, window and library function catalogs |
| `internal/engine/ydb/convert.go` | Direct ANTLR grammar paths for CASE, CAST, SELECT and grouping |
| `internal/codegen/golang/ydb_type.go` | Historical YQL-to-Go mappings |
| `internal/codegen/golang/query.go` | ParamsBuilder, optional and list binding |
| `internal/codegen/golang/templates/ydb-go-sdk/` | Native query API and execution options |

Current function typing is in `internal/yql/builtins`; current ANTLR semantic
walks remain in `internal/analyzer`. Unknown-type fallbacks and silently skipped
syntax from the historical implementation were deliberately not adopted.

The `engine-plugin` branch at `b9bf2b3c3d80aa6050e0511c85572944386d82f0` was also
reviewed. Its external engine protocol and omitted analyzer contradict the
standalone design. The `remove-ast-*` branches contain upstream AST maintenance,
not additional YDB semantics. These mechanisms are not dependencies or pending
imports. Their generic validation ideas are covered by the standalone CLI.

## Reproducing the export

The maintenance-only [importer](../../scripts/import-legacy-ydb.py) can recreate
this source snapshot from a local checkout containing the recorded revision:

```sh
python3 scripts/import-legacy-ydb.py /path/to/old/sqlc
```

Ordinary development and tests never run this importer and do not need the old
checkout. To reconstruct historical source, the patch can be applied to the
recorded base in a separate upstream sqlc checkout. The local patch and SQL
inputs remain readable even if the fork and all its branches are deleted.
