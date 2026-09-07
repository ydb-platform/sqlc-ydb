# Release plan

The first version is **0.0.1**. Preparing its code, changelog and release workflow
does not create a tag or publish a release. Consumer pilots and feedback belong
in versions below 1.0.0; 1.0.0 follows evidence from real projects.

This page owns release work and responsibilities. Current behavior is in
[compatibility](compatibility.md), and compiler feature design remains in
[the compiler roadmap](roadmap.md). Packaging commands are in
[releasing](releasing.md). Planned checks below are not claims of
completed acceptance.

## Before the first public release

| Work | Owner | Completion evidence |
| --- | --- | --- |
| Reliable analysis and generation | Implementation work | Regression tests for expressions, literals, nullability, names, column order and output ownership; unsupported cases fail explicitly. Known limitations remain documented. |
| Current Go SDK compatibility | Implementation work | Resolve the latest published stable ydb-go-sdk/v3, pin it in tests/examples, compile both native and database/sql outputs, and run their binding tests. Do not add runtime SDK imports to the generator module. |
| SDK maintainer review | User | Arrange reviews with the SDK maintainers in the team, collect their findings and agree which runtime profiles are ready for users. Implementation work addresses the findings. |
| Reproducible artifacts | Implementation work | Build and verify the six release archives, checksums, version and commit; run the release workflow without publishing first. Exercise the packaged executable, not only `go run`. |
| Module/repository naming | Implementation work | Resolve the historical `sqlc-engine-ydb` Go module path before the first tag; update internal imports, build scripts and references together if renamed. |
| User documentation on ydb.tech | Implementation work, near release | A reviewed SQLC section with a reproducible installation-to-query path and explicit compatibility boundaries, tested with release-candidate artifacts. |
| Publish the first release | User decision; prepared workflow | Start the manual publish workflow after a successful dry run. It assigns the version, prepares the changelog and creates the tag after artifact checks. No tag is created as part of preparing this plan. |

All selected SDK profiles need their applicable compilation and acceptance
checks. Fixes already made during the maintainability review cover several
blockers, but do not establish complete YQL semantics. Local-ydb images, runtime
suites and Docker builds remain sequential on each host.

## User documentation on ydb.tech

Publish the user guide in the SQLC section of ydb.tech when the release is ready
or close to ready. Introduce sqlc-ydb as the recommended YDB-specific variant
with the familiar sqlc workflow. Make its independent implementation and
supported subset clear. Link upstream documentation for verified applicable
recipes; describe YDB-specific or incompatible cases locally. Do not promise
that every upstream recipe works before compatibility tests establish that.

The first user journey should cover:

1. Select and download a platform archive; verify its checksum and version.
2. Write schema, named YQL queries and `sqlc.yaml`; generate a minimal project.
3. Install the chosen SDK dependencies and execute the generated methods.
4. Use parameters, optional values, result cardinality and transactions correctly.
5. Apply schema migration inputs, regenerate outputs and check them in CI.
6. Diagnose unsupported queries/options, naming conflicts and obsolete files.
7. Move from upstream sqlc configuration or the old YDB plugin configuration.

Use checked examples to establish which upstream recipes work, need YDB changes,
or are unsupported. A successful parse is not sufficient evidence. Include the
tested sqlc-ydb/SDK versions and link to their compatibility scope.

Keep architecture, contributor commands, SDK API evidence, technical contracts
and executable examples in this repository. Until the site guide exists, retain
the README quick start and target references. Once published, link the site from
the README rather than maintaining two complete user guides.

## External production-query corpus

The user may provide more than one million YDB queries. Corpus availability and
context are prerequisites, not assumed access. Initially keep this corpus and
its detailed reports outside the repository. Decide later whether selected,
minimized cases can become repository regressions.

**User responsibilities:** provide an approved input location and stable query
IDs, identify the available schema snapshots and parameter declarations, and
define the scope of the run. Query text alone is enough for a parser pass.
Semantic checks additionally need schema context, syntax settings and parameter
types; generation needs a query name, command/cardinality and selected runtime
options. Supply these as metadata when the original SQL has no sqlc annotations.
Parameter values, credentials and result rows are not needed.

**Future implementation work, once inputs are available:**

- Run separate stages: parse, local analysis, generation, SDK compilation. An
  optional server type check requires separate API research and explicit opt-in;
  the runner must not execute production queries.
- Start with a representative small sample, then a larger sample, then the full
  corpus. Stream input, use bounded workers and batches, cache immutable catalogs
  by schema/context hash, and checkpoint progress. Do not start a process or
  rebuild the schema catalog for every query. A long-lived worker process can
  provide hard timeout/RSS limits and crash isolation where in-process cancellation
  is insufficient.
- Reuse the current analyzer and generators. Extract schema and query phases from
  `analyzer.Analyze` only when needed for catalog reuse, preserving its existing
  entry point. No second AST, plugin interface or universal DBMS harness is needed.
- Record a JSONL outcome for every input ID and stage: success, missing context,
  unsupported feature, semantic error, generation error, SDK compilation error,
  timeout, resource limit, crash or infrastructure failure. Introduce stable
  diagnostic codes rather than grouping by human-readable error strings.
- Record code/parser/SDK/compiler versions, query/context/config hashes, limits
  and run ID. Preserve the input denominator, including skipped queries and
  failures. Deduplicate execution only when all stage inputs match exactly;
  expand the outcome back to each original ID. Similar-query grouping is for
  investigation, not a substitute for checking those inputs.
- Compile generated code in bounded shards against each selected SDK and narrow
  failing shards to individual cases. Report results per stage and runtime;
  parsing a million queries does not prove correct inferred types or runtime behavior.
- Turn confirmed implementation defects into small regression tests when their
  inputs can be shared. Do not publish the original corpus by default.

Today the relevant public commands are `compile`, `generate` and `diff`.
`compile` performs local semantic analysis; `vet`, `check` and `parse` are not
implemented CLI commands. The first corpus runner can call internal stages
without inventing aliases or changing that public contract.

Complete generation of the corpus is a long-term coverage target. Report the
current unsupported subset honestly; do not weaken diagnostics to increase the
success rate. Remaining work includes computed expressions, casts, function
typing, CTEs/subqueries, multiple result sets, broader DDL and type coverage,
nullability evidence and schema drift. The current inventory is in
[compatibility](compatibility.md).

## Consumer acceptance before 1.0.0

**User responsibilities:** select two or three real consumer projects, arrange
SDK maintainer reviews and coordinate installation/use from the user documentation
during 0.x releases. Collect concrete API, compatibility and documentation findings
and decide whether the product is ready for 1.0.0.

**Implementation work:** reproduce reported failures, fix confirmed defects,
add shareable minimized regressions, update documentation and publish fixes
through the prepared release process when authorized. Do not mark this gate
complete based only on the repository's authors example or a large parser pass.
