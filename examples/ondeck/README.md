# Ondeck

This is a YDB adaptation of all PostgreSQL, MySQL, and SQLite variants of the
upstream sqlc `ondeck` example. The ordered `schema` directory retains the
rename, add-column, and drop-column migration sequence. The dependent ALTERs
are separate files because YDB compiles every statement in one request before
executing the rename.

YDB uses `Utf8` for the upstream status enum. Applications must restrict
`status` values to `op!en` or `clo@sed`, the values in the PostgreSQL variant.
The PostgreSQL array fields and the serialized MySQL/SQLite fields converge on
nullable YQL `Json` columns: callers pass JSON arrays of strings in `statuses`
and `tags`, preserving values, order, and the single `CreateVenue` operation.
Applications validate the array element values.

This schema omits the upstream foreign-key, comment, serial, default-value and
check constraints. `CreateVenue` takes an explicit `Uint64` ID and an
optional runtime `Timestamp`; status validation stays in the application. The
new migration column is nullable so the migration is valid for existing rows.
PostgreSQL `RETURNING` operations are retained, MySQL/SQLite `:execresult` maps
to `:one ... RETURNING id`, and positional parameters become named YQL parameters.
The grouped count names its result and groups by `city` instead of an ordinal.
