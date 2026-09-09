# Decisions

These choices constrain maintenance; implementation details remain in the linked
documents. Revisit a decision explicitly rather than letting a local workaround
change the architecture.

| Decision | Reason and reference |
| --- | --- |
| Independent YDB-only implementation | Compatibility concerns user workflow, not upstream internal code or Git history. See [compatibility](../docs/compatibility.md). |
| Keep semantic analysis; no intermediate AST | Direct ANTLR contexts avoid a second syntax representation while resolved types remain necessary for code generation. See [architecture](../docs/architecture.md). |
| Built-in generators only | New language support belongs in this repository; external engine/codegen/WASM/process plugins are deliberately excluded. See [compatibility](../docs/compatibility.md). |
| One C# provider with ADO.NET, Dapper and linq2db profiles | All profiles use the official ADO.NET provider; a separate nominally native profile would duplicate it. See [C#](../docs/csharp.md). |
| SQL-first framework adapters | Java and C# frameworks receive typed query methods and projections, not inferred ORM entities or translated LINQ expressions. See [Java](../docs/java.md) and [C#](../docs/csharp.md). |
| Separate API field names from result-set keys | JOIN result keys can include table qualifiers. Name-based decoders use `Column.ResultName()`; positional decoders retain projection order. See [architecture](../docs/architecture.md). |
| Derive SDK execution SQL before generation | Keep original SQL intact; derive `SQLWithoutDeclarations` once from ANTLR tokens for SDKs that reconstruct declarations. Do not strip declarations independently in each renderer. See [architecture](../docs/architecture.md). |
| Preserve YQL types and exact values through SDK boundaries | Typed wrappers and raw decoding are intentional when native language values erase YQL types or SDK conversions lose precision. Recheck the documented constraint before removing them. See [targets](../docs/targets.md) and its language references. |
| Share example dependencies by language | Keep generated outputs inside each example's language/runtime directories and reuse dependency manifests and harnesses across example families. See [development](../docs/development.md#generated-runtime-checks). |
| Shared macro processing before generators | Language count must not multiply SQL semantic work. A separate compiler package is optional; macros are still planned. See [roadmap](../docs/roadmap.md). |
| Offline generation by default | Database-assisted analysis is deferred until a concrete need defines the API and semantics. See [roadmap](../docs/roadmap.md). |
| Sequential local-ydb validation per host | Concurrent images, runtime suites and container builds have exceeded available memory. See [development](../docs/development.md). |
| User guide on ydb.tech near release | Keep technical references and examples here; publish the consumer journey with verified upstream recipes and explicit limits on the YDB site. See [release plan](../docs/release-plan.md). |
| Consumer acceptance in 0.x before 1.0.0 | The user arranges SDK reviews, production-query corpus access and real-project pilots; implementation work addresses the resulting findings. See [release plan](../docs/release-plan.md). |
| Manual publication from accumulated changelog entries | The maintainer chooses the version part in the Actions form. The workflow assigns the version and checks all artifacts before pushing the release commit/tag; RCs preserve pending notes. See [releasing](../docs/releasing.md). |

SDK-specific behavior should be reviewed with the SDK maintainers when its public
contract is unclear. They are available within the product team; invented fallback
behavior is not a substitute for establishing that contract.
