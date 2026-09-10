# Release readiness snapshot

This maintainer snapshot records evidence checked on **2026-09-10**. The first
stable version remains **0.0.1**. Release candidates gather consumer feedback;
1.0.0 still requires evidence from real projects.

Treat the evidence levels separately:

- **Implemented** means the repository contains the code, tests or workflow.
- **CI-verified** means the cited GitHub Actions run completed successfully for
  its exact commit and dependencies.
- **Release-verified** means a publish run built its artifacts and created a tag
  or draft release. A draft prerelease is not a published stable release.
- **External** gates require a reviewer, documentation publication or consumer
  pilot outside this repository. Passing repository CI does not complete them.

Publication commands and recovery rules are in [release operations](releasing.md).

## Evidence checked

| Gate | Status on 2026-09-10 | Evidence and remaining work |
| --- | --- | --- |
| Analysis and generation | **Implemented and CI-verified on current `main`** | The direct ANTLR/semantic pipeline and built-in generators are described in [architecture](architecture.md). [CI run 34410584094](https://github.com/ydb-platform/sqlc-ydb/actions/runs/34410584094) passed the standalone, YDB acceptance and C++ acceptance jobs at `9cbbfb2eda1c196b3a35bbe7dd3282c2b8de28e5`. Unsupported behavior remains bounded by [compatibility](../docs/compatibility.md); new findings still need regressions. |
| SDK compatibility | **Automated pinned-SDK gate complete; maintainer review incomplete** | The current main CI run compiled the published C#, Java and Rust SDK targets, all Dapper/linq2db examples, and the JavaScript, Rust and PHP examples. It ran sequential YDB acceptance for Go, Python, C#, Java, Dapper, linq2db, JavaScript, Rust, PHP and C++ native/userver. The Go examples pin `v3.151.1`, which was also the latest stable ydb-go-sdk release when checked. Exact pins and inspected API sources are in [SDK evidence](sdk-evidence.md). Eight generated-output review PRs, [#2 through #9](https://github.com/ydb-platform/sqlc-ydb/pulls), were open with no submitted reviews; SDK maintainer findings and profile readiness therefore remain external and incomplete. |
| Reproducible artifacts | **Implemented and release-verified for RC2; current workflow revision not yet publish-run** | [Publish run 34357127337](https://github.com/ydb-platform/sqlc-ydb/actions/runs/34357127337) succeeded at `d8f08f605e21edd09fbf637b448f735f15007dbe`, built and verified all six archives, and ran each archive on its matching Linux, macOS or Windows host before creating `v0.0.1-rc2`. Current `main` later removed the cross-host smoke matrix: its workflow executes Linux/amd64 and checks archive contents and Go metadata for the other five targets. No publish run was found for current `main` after that change. Run a dry run on the intended release commit before another RC or stable release. |
| Repository and module name | **Decided and aligned** | The final repository is `ydb-platform/sqlc-ydb`; the module is `github.com/ydb-platform/sqlc-ydb`. Source imports and release module checks use that path. |
| User documentation | **Remaining** | Repository guides and examples exist, but no reviewed ydb.tech SQLC guide or release-candidate walkthrough is linked from the repository. A targeted site search found no sqlc-ydb guide. Draft, review and run the guide against the selected RC artifacts before treating this gate as complete. |
| Publication | **RC artifacts exist; stable release remaining** | Tags `v0.0.1-rc0`, `v0.0.1-rc1` and `v0.0.1-rc2` exist. GitHub lists all three releases as draft prereleases, with no publication time. No stable tag or public stable release was found. Starting a stable publish remains a user action after the other gates pass. Preparing code, notes or this audit does not authorize publication. |

The three draft RCs prove that the release machinery has created recoverable
artifacts. They do not prove SDK maintainer approval, the ydb.tech guide, corpus
coverage, consumer adoption or readiness for 1.0.0.

## User documentation on ydb.tech

Publish the SQLC guide near release. Introduce sqlc-ydb as the YDB-specific
implementation of the familiar sqlc workflow, with explicit coverage limits.
Use verified upstream recipes by reference and describe YDB differences locally.

Cover installation and checksum/version checks; schema, query and configuration
inputs; generation and SDK setup; parameters, nullability, cardinality and
transactions; migration inputs and CI diffs; diagnostics and obsolete outputs;
and migration from upstream sqlc or the old YDB plugin configuration. Include
tested sqlc-ydb/SDK versions and link to their compatibility scope.

Keep technical contracts, SDK source references and executable examples in this
repository. Once the site guide exists, link it from the README instead of
maintaining a second complete user journey.

## External query corpus

The user may supply more than one million production queries. This work has not
started because an approved input location and the required context are not
available. The run depends on stable query IDs, scope, schema snapshots, syntax
settings and parameter declarations. Parsing needs only query text; semantic
analysis needs schema and type context; generation also needs query names,
cardinality and target options. Parameter values, credentials and result rows
are unnecessary.

Keep raw queries and detailed reports outside this repository. Once inputs are
available, implement the runner around the current analyzer and generators:

- Start with a representative sample, then scale through bounded workers and
  batches with checkpoints. Reuse catalogs by schema/context hash; do not rebuild
  the schema or start a process per query. Enforce timeout and memory limits,
  using long-lived worker processes where crash isolation is necessary.
- Record a JSONL outcome per input and stage: parse, analysis, generation and SDK
  compilation. Distinguish missing context, unsupported features, semantic and
  generation errors, compiler failures, timeouts, resource limits, crashes and
  infrastructure failures with stable diagnostic codes.
- Record code/parser/SDK/compiler versions, input/context/config hashes, limits
  and run ID. Preserve every input in the denominator. Deduplicate execution only
  for identical stage inputs and expand outcomes back to the original IDs.
- Compile outputs in bounded shards for each selected SDK and isolate failing
  cases. Report each stage and runtime separately; parsing success does not prove
  semantic or runtime correctness.

The runner can call internal stages; the public commands remain `compile`,
`generate` and `diff`. Extract schema/query phases only when catalog reuse needs
them, preserving `analyzer.Analyze` as the entry point.

Server checks require the explicit mode described in [the roadmap](roadmap.md).
The runner must not execute production queries. Add minimized, shareable cases
for confirmed defects; retain strict diagnostics for unsupported queries.

## Consumer acceptance before 1.0.0

The user selects two or three consumer projects, coordinates SDK reviews and
pilots, and decides readiness for 1.0.0. No pilot result or real-project adoption
evidence was found in the repository or GitHub review state on 2026-09-10.
Maintainers reproduce findings, add regressions, fix code and documentation, and
prepare releases. Repository examples, draft RCs or a large parser pass alone do
not satisfy this gate.
