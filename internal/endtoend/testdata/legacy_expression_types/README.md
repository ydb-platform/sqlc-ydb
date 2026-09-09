# Legacy expression types

Adapted from [testdata/legacy-ydb/inputs/cast_coalesce/ydb/stdlib/query.sql](../../../../testdata/legacy-ydb/inputs/cast_coalesce/ydb/stdlib/query.sql), [testdata/legacy-ydb/inputs/case_named_params/ydb/query.sql](../../../../testdata/legacy-ydb/inputs/case_named_params/ydb/query.sql), and
[testdata/legacy-ydb/inputs/builtins/ydb/query.sql](../../../../testdata/legacy-ydb/inputs/builtins/ydb/query.sql). The preserved inputs remain
byte-for-byte historical source material. This fixture replaces the legacy
`Text` table definition with `Utf8`, adds explicit YQL declarations, and
combines nullable `CASE`, `CAST`, `COALESCE`, `LENGTH`, and `ABS` into one
current CLI configuration for Go `database/sql` and Python YDB output.
