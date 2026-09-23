# Namespaces

Two catalogs contain a table named `users`. Each named query has an explicit static `TablePathPrefix`, so its unqualified table reference selects the intended catalog. `CompareUserNames` combines a relative reference with an absolute path into the other catalog; `FindPrimaryUsersByName` selects a secondary index. The schema uses one prefix per source file. The example generates all built-in language/runtime profiles.

The checked-in paths target the `/local` database of local-ydb. For another database or environment, replace `/local/sqlc_namespaces/a` and `/local/sqlc_namespaces/b` consistently in the schema and queries, then regenerate. The prefix is part of the compiled SQL, not a generated method argument or an environment-variable substitution. Use an absolute path including the database name. A relative TablePathPrefix is not relative to the connection's database.

`GetPrimaryUser` and `FindPrimaryUsersByName` demonstrate offline parameter inference. `GetSecondaryUser` and both writes declare their parameters explicitly. Connected analysis requires declarations for every external parameter; add them to the inferred queries before using a database configuration.

The schema defines two independent tables, and generated catalog models retain their full path when deriving unique names. SQL aliases and result field names retain their query meanings. See [table path resolution](../../docs/compatibility.md#table-path-resolution) for the supported pragma forms and [database-assisted analysis](../../docs/database-analysis.md) for schema checking and discovery.

Run `make generate` from the repository root. The Go examples compile under the shared examples module. Runtime checks use disposable tables and run sequentially; see [development](../../.agents/development.md).
