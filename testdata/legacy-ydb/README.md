# YDB SQL regression corpus

This directory contains SQL and configuration inputs from the YDB work in
`ydb-platform/sqlc` at commit `8eed5d890396eb03953248a3ec4ab7e28dfaed45`.
The corpus runs independently of the original repository.

## Contents

- [manifest.json](manifest.json) records source paths, revision, SHA256 checksums
  and the mapping from 181 original configurations to 131 distinct combinations
  of schema/query inputs across 126 scenario names.
- `inputs/` preserves the original SQL and configurations byte for byte,
  including driver variants. Old configuration options are not treated as the
  current compatibility contract.
- `expected/` records the current analyzer result for every distinct input:
  resolved catalog/parameters/result columns, or exact source diagnostics.
  Rejected cases remain visible coverage gaps or invalid inputs; they are
  neither skipped nor counted as implemented YQL support.

## Executable tests and adaptations

`internal/endtoend/legacy_test.go` checks every input file hash and runs the
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
expectations against the YQL contract. Keep original inputs immutable and place
adaptations in ordinary CLI fixtures:

- [CASE, CAST and scalar functions](../../internal/endtoend/testdata/legacy_expression_types)
- [grouped aggregates and HAVING](../../internal/endtoend/testdata/legacy_aggregate_having)
- [UNION and UNION ALL](../../internal/endtoend/testdata/legacy_union)
- [native Go lists, optional elements, Decimal and Uuid](../../internal/endtoend/testdata/legacy_native_go_types)

Each adaptation records its original scenarios and changes, including replacing
inherited PostgreSQL types such as `Serial` with actual YQL types and making
parameter declarations explicit. Macros, subqueries and other unsupported
constructs remain explicit errors until implemented with their own tests.

Current function typing is in `internal/yql/builtins`; direct ANTLR semantic
walks are in `internal/analyzer`. See [provenance](../../docs/provenance.md) for
implementation references and [compatibility](../../docs/compatibility.md) for
the supported subset.

## Reproducing the import

The maintenance-only [importer](../../scripts/import-legacy-ydb.py) can recreate
the SQL/config inputs and manifest from a local checkout containing the recorded
revision:

```sh
python3 scripts/import-legacy-ydb.py /path/to/old/sqlc
```

Ordinary development and tests never run this importer and do not need the old
checkout. Semantic expectations are maintained separately by the test runner.
