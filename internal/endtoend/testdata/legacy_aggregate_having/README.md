# Legacy aggregate and HAVING

Adapted from [testdata/legacy-ydb/inputs/having/ydb/query.sql](../../../../testdata/legacy-ydb/inputs/having/ydb/query.sql) and
[testdata/legacy-ydb/inputs/having/ydb/schema.sql](../../../../testdata/legacy-ydb/inputs/having/ydb/schema.sql). It keeps the original
`GROUP BY city` and aggregate `HAVING` purpose, while replacing `Text` with
`Utf8`, renaming the temperature column, and declaring the YQL parameter
explicitly for the current CLI configuration.

The source corpus is available under the MIT license in
[testdata/legacy-ydb/LICENSE](../../../../testdata/legacy-ydb/LICENSE) (Copyright (c) 2024 Riza, Inc.).
