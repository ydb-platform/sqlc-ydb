# YQL implementation evidence

These source references support the analyzer's documented [schema migration coverage](../docs/compatibility.md#schema-migration-coverage). They record the implementation examined, not a claim about the latest YQL revision.

YQL main at `d62403dadf7588c33d2d0a61296a157b61163d52` explicitly handles [`DROP TABLE IF EXISTS` through `missingOk`](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L575) and [rejects combining RENAME TO with other ALTER actions](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L2399).
