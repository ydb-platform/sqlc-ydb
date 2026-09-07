# YDB golden tests

Each directory in `testdata` is a standalone YDB-only input. Positive fixtures
contain `expected/` generated files; negative fixtures contain `stderr.txt`.
The runner copies inputs to a temporary directory and invokes the real CLI, so
fixtures cover configuration, source loading, analysis, and generation together.

Run `go test ./internal/endtoend`. Update a deliberate baseline with
`go test ./internal/endtoend -update` and review the resulting files alongside
the SQL and implementation changes. Tests never update snapshots by default,
and update mode never converts a failing positive fixture into an accepted
negative case. New negative cases must explicitly contain `stderr.txt`.

The design borrows fixture discovery and exact generated-file comparisons from
sqlc upstream at commit `23e357a414310aa8846e64624da8b8a626b3a610`:
[endtoend_test.go](https://github.com/sqlc-dev/sqlc/blob/23e357a414310aa8846e64624da8b8a626b3a610/internal/endtoend/endtoend_test.go),
[case_test.go](https://github.com/sqlc-dev/sqlc/blob/23e357a414310aa8846e64624da8b8a626b3a610/internal/endtoend/case_test.go), and
[CI workflow](https://github.com/sqlc-dev/sqlc/blob/23e357a414310aa8846e64624da8b8a626b3a610/.github/workflows/ci.yml).
It intentionally omits upstream's multi-engine Docker/native setup, plugins,
process hooks, and test contexts; sqlc-ydb supports only YDB.
