# Jets

This adapts the upstream sqlc `jets` example, whose schema and queries come from the SQLBoiler README, to YDB.

PostgreSQL `integer` and `text` become YQL `Int32` and `Utf8`. YDB does not use the example's foreign-key constraints, so the relationships remain visible in the `pilot_id` and `language_id` columns and the composite primary key. The positional delete parameter becomes a named, explicitly declared YQL parameter. `COUNT(*)` has an explicit result name for generated APIs.
