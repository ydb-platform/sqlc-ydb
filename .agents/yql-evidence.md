# YQL implementation evidence

These source references support the analyzer's documented [schema migration coverage](../docs/compatibility.md#schema-migration-coverage). They record the implementation examined, not a claim about the latest YQL revision.

YQL main at `d62403dadf7588c33d2d0a61296a157b61163d52` explicitly handles [`DROP TABLE IF EXISTS` through `missingOk`](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L575) and [rejects combining RENAME TO with other ALTER actions](https://github.com/ydb-platform/ydb/blob/d62403dadf7588c33d2d0a61296a157b61163d52/yql/essentials/sql/v1/translation/sql_query.cpp#L2399).

## AS_TABLE and DML projections

The [AS_TABLE reference](https://ydb.tech/docs/en/yql/reference/syntax/select/from_as_table) requires explicit column lists on both INSERT and SELECT sides when specifying target columns. AS_TABLE source column order is not guaranteed by its struct declaration. The analyzer therefore rejects wildcard INSERT/UPSERT SELECT projections with an explicit target list. Struct equality compares named field sets independently of declaration order while retaining source order for generated representations. See [expression type rules](https://ydb.tech/docs/en/yql/reference/syntax/expressions).

On 2026-09-15, local YDB (`ydbplatform/local-ydb:nightly`, image digest `sha256:16fc066d4944ce7b4bf73ecd8732dcc077e2b8303075f4470a616113ad381af4`) confirmed that both INSERT and UPSERT with an explicit target list map explicit SELECT expressions by position, including when their aliases are reversed. The server emitted alias-mismatch warnings but stored the first projected value in the first listed target column. This evidence is specific to the explicit-target form: it does not establish positional matching for statements without a target list.

UPDATE/DELETE ON SELECT instead validate the source's named columns and require the complete primary key; their star expansion is used only for named matching, never to infer Struct field order. `TestLiveYDBTypedDML` executes generated Go code for both native and database/sql adapters, including reordered AS_TABLE fields, partial updates, absent keys, filters, empty lists and a standalone Struct key. It passed against the same local image on 2026-09-15 with SDK v3.151.1.

## Function contracts

`TestLiveYDBSemanticTypes` compares the analyzer's result types against server metadata. On the same local image it confirmed all 21 shipped Digest functions with plain and optional inputs, including omitted, NULL and named seeds where supported. A seed's optionality does not itself make the return type optional; AutoMap on the input does. These are offline signatures, not UDF implementations. User-defined contracts belong to one SQL configuration entry and do not execute or install code.

The existing collection predicate is covered by the [dictionary functions](https://ydb.tech/docs/en/yql/reference/builtins/dict) and [Yson conversion](https://ydb.tech/docs/en/yql/reference/udf/list/yson) contracts. Live `TypeOf` probes confirmed that `Yson::ConvertToStringList` with nullable Json or Yson input returns a required `List<String>`, whereas `ToSet` of an optional list and `SetIsDisjoint` with optional collection inputs retain optionality. Resource-valued Yson overloads remain outside this implementation.
