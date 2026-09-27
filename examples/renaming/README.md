# Renaming generated Go fields

Both Go runtimes use `gen.go.rename` to generate `AccountID` and `Label` fields for the SQL columns `account_id` and `display_name`, plus an `Account` model for the `accounts` table. The same column mapping applies to query parameter structs, result rows, and embedded table models; SQL parameter and result names and YDB Struct member names remain unchanged. `json_tags_case_style: camel` independently changes their JSON tags to `accountId` and `displayName`.

Run `make generate` from the repository root, then inspect `go/native` and `go/database/sql`.
