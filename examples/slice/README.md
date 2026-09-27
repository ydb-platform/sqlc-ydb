# Typed list filters with `sqlc.slice`

The [queries](queries.sql) use the upstream `IN (sqlc.slice("ids"))` spelling for a collection parameter. sqlc-ydb lowers it to ``IN $`ids` `` before YQL analysis and generation. YDB receives one `List<Uint64>` parameter, including for an empty collection; it does not expand one placeholder per element. The analyzer infers `Uint64` from `records.id`.

The [configuration](sqlc.yaml) generates the same query for both Go runtimes and Rust. Each generated method binds a typed list. Other runtimes can use this macro only if their existing generator supports the resolved scalar list type; unsupported list bindings fail generation with a target-specific error.
