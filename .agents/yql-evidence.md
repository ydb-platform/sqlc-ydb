# YQL implementation evidence

These source references support the analyzer's documented [schema migration coverage](../docs/compatibility.md#schema-migration-coverage). They record the implementation examined, not a claim about the latest YQL revision.

YQL main at `d62403dadf7588c33d2d0a61296a157b61163d52` explicitly handles [`DROP TABLE IF EXISTS` through `missingOk`](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L575) and [rejects combining RENAME TO with other ALTER actions](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L2399).

## AS_TABLE insert projection

The [AS_TABLE reference](https://ydb.tech/docs/en/yql/reference/syntax/select/from_as_table) requires explicit column lists on both INSERT and SELECT sides when specifying target columns. AS_TABLE source column order is not guaranteed by its struct declaration. The analyzer therefore rejects wildcard INSERT SELECT projections and resolves explicit projection fields from the declared struct. Struct equality compares named field sets independently of declaration order while retaining source order for generated representations. See [expression type rules](https://ydb.tech/docs/en/yql/reference/syntax/expressions).
