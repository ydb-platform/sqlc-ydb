# Release plan

The first stable version is **0.0.1**. Consumer pilots and feedback belong in
0.x releases; 1.0.0 requires evidence from real projects. This page tracks
outstanding gates, not completed acceptance. Packaging and publication commands
are in [releasing](releasing.md).

## Before the first stable release

| Work | Owner | Completion evidence |
| --- | --- | --- |
| Analysis and generation | Maintainers | Semantic regressions, generated-output checks and documented unsupported cases. |
| SDK compatibility | Maintainers | Resolve and pin the latest stable Go SDK; compile and test native and `database/sql` outputs. Run every selected profile's compilation and sequential runtime checks against its pinned SDK. |
| SDK review | User | Collect findings from SDK maintainers and agree which profiles are ready; maintainers fix confirmed defects. |
| Reproducible artifacts | Maintainers | A successful publish dry run checks six archives, checksums, version/commit metadata and packaged executables on native runners. |
| Repository/module name | Maintainers | Confirm the published name; if renamed, update `go.mod`, imports, scripts and documentation together. |
| User documentation | Maintainers | Review the ydb.tech SQLC guide and run its examples with release-candidate artifacts. |
| Publication | User | Start the manual workflow after the gates pass. Preparing code or notes does not authorize publication. |

## User documentation on ydb.tech

Publish the SQLC guide near release. Introduce sqlc-ydb as the YDB-specific
implementation of the familiar sqlc workflow, with explicit coverage limits.
Use verified upstream recipes by reference and describe YDB differences locally.

Cover installation and checksum/version checks; schema, query and configuration
inputs; generation and SDK setup; parameters, nullability, cardinality and
transactions; migration inputs and CI diffs; diagnostics and obsolete outputs;
and migration from upstream sqlc or the old YDB plugin configuration.
Include tested sqlc-ydb/SDK versions and link to their compatibility scope.

Keep technical contracts, SDK source references and executable examples here.
Once the site guide exists, link it from the README instead of maintaining a
second complete user guide.

## External query corpus

The user may supply more than one million production queries. The run depends
on an approved input location, stable query IDs, scope, schema snapshots, syntax
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
pilots, and decides readiness for 1.0.0. Maintainers reproduce findings, add
regressions, fix code and documentation, and prepare releases. Repository
examples or a large parser pass alone do not satisfy this gate.
