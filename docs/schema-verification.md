# Verify schema changes against released queries

`sqlc-ydb verify` checks whether the analyzed YQL used by an already released client remains valid against a proposed YDB schema. Keep a copy of the released `sqlc.yaml` and its referenced schema and query files, for example in a release tag or archive. The proposed configuration is the ordinary current `sqlc.yaml`.

```sh
sqlc-ydb verify --against path/to/released/sqlc.yaml -f sqlc.yaml --no-database
```

Both configurations need local `schema` inputs. The released schema is used to resolve the released queries and their executable YQL before runtime-specific rendering, including expanded wildcards and lowered macros. The proposed schema and current queries are checked first; then every released query is analyzed against the proposed schema, even if that query has been removed from the current configuration. A change to a released query's resolved parameter type, result-set count, result-column name, order or type fails verification. The command writes no generated files and never applies migrations.

Query sets with a nonempty `sql[].name` are matched by name, independently of their order. An unnamed released query set matches the unnamed proposed set at the same position. Every released set must have a match; additional proposed sets are allowed. Keep released function signatures and configured parameter types in the released configuration because they form part of its analysis contract.

For server validation, configure `database.uri` in the proposed configuration and omit `--no-database`. Point it at an isolated disposable YDB database **after applying the proposed schema there**. Verification checks schema drift and sends the current and released queries to YDB in non-executing `EXPLAIN` mode. Functions declared in the released `analyzer.functions` must also be available in that database; otherwise use `--no-database` for offline verification. The released configuration is always analyzed offline against its saved schema; it does not need the old database. Without a proposed database connection, verification checks the analyzer's supported YQL subset offline and cannot establish that YDB accepts every query.

Verification protects the shared YQL and resolved input/result type contract, not runtime-specific rendering or arbitrary application code. It cannot prove data migration correctness, performance, runtime permissions or that a transaction succeeds. It does not store release snapshots or compare historical schemas automatically: supply the exact released configuration and files you deployed.
