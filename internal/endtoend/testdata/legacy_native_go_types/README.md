# Legacy native Go types

Adapted from [testdata/legacy-ydb/inputs/select_text_array/ydb-go-sdk/query.sql](../../../../testdata/legacy-ydb/inputs/select_text_array/ydb-go-sdk/query.sql),
[testdata/legacy-ydb/inputs/types_uuid/ydb/ydb-go-sdk/schema.sql](../../../../testdata/legacy-ydb/inputs/types_uuid/ydb/ydb-go-sdk/schema.sql), and
[testdata/legacy-ydb/inputs/datatype/ydb/ydb-go-sdk/sql/numeric.sql](../../../../testdata/legacy-ydb/inputs/datatype/ydb/ydb-go-sdk/sql/numeric.sql). It uses
explicit `List<Uint64>`, `List<Optional<Uint64>>`, `Decimal(22, 9)`, and `Uuid`
parameters so the native Go YDB generator must bind each type. This stays
native-Go-only: the `database/sql` target does not support List parameters or results.

The source corpus is available under the MIT license in
[testdata/legacy-ydb/LICENSE](../../../../testdata/legacy-ydb/LICENSE) (Copyright (c) 2024 Riza, Inc.).
