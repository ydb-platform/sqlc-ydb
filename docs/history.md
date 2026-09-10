# Project history

sqlc-ydb is an independent YDB query-code generator inspired by
[sqlc](https://github.com/sqlc-dev/sqlc). The repository is
[`ydb-platform/sqlc-ydb`](https://github.com/ydb-platform/sqlc-ydb).
Dates below are UTC; upstream status was checked on 2026-09-10.

## Upstream proposals

All three proposals were opened by [@asmyasnikov](https://github.com/asmyasnikov).

| Date | Proposal | Outcome |
| --- | --- | --- |
| 2025-09-03 | [#4090: built-in YDB support](https://github.com/sqlc-dev/sqlc/pull/4090) | [@kyleconroy declined the addition that day](https://github.com/sqlc-dev/sqlc/pull/4090#issuecomment-3249587294), citing the cost of maintaining additional engines, and suggested a fork. The draft was closed unmerged on 2026-06-08 after an automated conflict notice. |
| 2025-10-28 | [#4158: extensible engine architecture](https://github.com/sqlc-dev/sqlc/issues/4158) | Proposed adding engines without modifying sqlc core. The issue remains open, without a maintainer reply. |
| 2025-12-28 | [#4247: external engine plugins](https://github.com/sqlc-dev/sqlc/pull/4247) | A subprocess protocol would pass resolved query metadata to code generation. [@asmyasnikov reported a working YDB MVP on 2026-02-01](https://github.com/sqlc-dev/sqlc/pull/4247#issuecomment-3830761446). [@kyleconroy rejected the design on 2026-08-18](https://github.com/sqlc-dev/sqlc/pull/4247#issuecomment-5332956790) because upstream was rebuilding analysis differently. The PR was closed unmerged that day. |

Upstream's existing code-generation plugins consume already-analyzed queries;
they cannot add a SQL dialect. The subsequent
[internal analysis core](https://github.com/sqlc-dev/sqlc/pull/4521) did not
provide the external-engine protocol proposed in #4247.

## Standalone implementation

On 2026-09-07, this repository switched to a standalone generator
(commit `57ebcdd243500f56168e71d27057123a8b872dda`). The decisions were:

- Maintain an independent implementation, using selected upstream code and tests
  as references rather than following upstream source merges.
- Support YDB only. Analyze ANTLR YQL parse contexts directly, without an
  intermediate AST; retain a separate semantic-analysis stage.
- Embed language generators. Exclude external engine and code-generation plugins.
- Preserve familiar CLI and configuration contracts where applicable. Track
  differences in the [compatibility contract](compatibility.md), with independent
  versions and no upstream compatibility-version label.
