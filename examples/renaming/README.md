# Renaming generated Go fields

Both Go runtimes use `gen.go.rename` to generate `AccountID` and `Label` fields for the SQL columns `account_id` and `display_name`. The same mapping applies to query parameter structs, result rows, and embedded table models; SQL parameter and result names, YDB Struct member names, and JSON tags remain unchanged.

Run `make generate` from the repository root, then inspect `go/native` and `go/database/sql`.
