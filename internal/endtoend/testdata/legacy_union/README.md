# Legacy UNION

Adapted from [testdata/legacy-ydb/inputs/select_union/ydb/ydb-go-sdk/query.sql](../../../../testdata/legacy-ydb/inputs/select_union/ydb/ydb-go-sdk/query.sql)
and [testdata/legacy-ydb/inputs/order_by_union/ydb/query.sql](../../../../testdata/legacy-ydb/inputs/order_by_union/ydb/query.sql). It retains both
distinct `UNION` and `UNION ALL`, replacing legacy `Text` and `BigSerial` with
explicit current YQL `Utf8` and `Int64` columns. The nullable and required
`label` branches also make the merged result nullability part of the fixture.

The source corpus is available under the MIT license in
[testdata/legacy-ydb/LICENSE](../../../../testdata/legacy-ydb/LICENSE) (Copyright (c) 2024 Riza, Inc.).
