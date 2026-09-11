# Decisions

These choices constrain maintenance; implementation details remain in the linked
documents. Revisit a decision explicitly rather than letting a local workaround
change the architecture.

| Decision | Reason and reference |
| --- | --- |
| Historical tests belong to current owning suites | Historical tests came from `ydb-platform/sqlc@8eed5d890396eb03953248a3ec4ab7e28dfaed45`; adapted cases belong to owning test suites, and the bulk snapshot corpus must not be restored. See [provenance](../docs/provenance.md). |
| Independent YDB-only implementation | Compatibility concerns user workflow, not upstream internal code or Git history. See [compatibility](../docs/compatibility.md). |
| Keep semantic analysis; no intermediate AST | Direct ANTLR contexts avoid a second syntax representation while resolved types remain necessary for code generation. See [architecture](architecture.md). |
| Built-in generators only | New language support belongs in this repository; external engine/codegen/WASM/process plugins are deliberately excluded. See [compatibility](../docs/compatibility.md). |
| One C# provider with ADO.NET, Dapper and linq2db profiles | All profiles use the official ADO.NET provider; a separate nominally native profile would duplicate it. See [C#](../docs/csharp.md). |
| SQL-first framework adapters | Named SQL determines generated methods. The jOOQ prototype translates those statements to typed DSL using the analyzed ANTLR contexts. It does not invent entity CRUD operations. ORM contracts for Spring/Hibernate remain deferred in [issue #12](https://github.com/ydb-platform/sqlc-ydb/issues/12). See [Java](../docs/java.md). |
| Separate API field names from result-set keys | JOIN result keys can include table qualifiers. Name-based decoders use `Column.ResultName()`; positional decoders retain projection order. See [architecture](architecture.md). |
| TypeScript DTOs preserve result column names | Result properties use exact SDK keys, with quoted properties for qualified names. This avoids SQL alias changes and runtime name mapping. Method and parameter names retain their TypeScript naming conventions. |
| Derive SDK execution SQL before generation | Keep original SQL intact; derive `SQLWithoutDeclarations` once from ANTLR tokens for SDKs that reconstruct declarations. Do not strip declarations independently in each renderer. See [architecture](architecture.md). |
| Follow each target's documented SDK value contract | Typed bindings preserve YQL parameter types. The maintainer-approved TypeScript API uses SDK-native Date and parsed JSON results; other targets retain their documented precision guarantees. See [targets](../docs/targets.md) and its language references. |
| SQL at its execution site | Emit readable indented SQL literals directly in query/execute/prepare calls, without separate generated SQL constants. Language escaping must preserve literal contents and control bytes. |
| Share example dependencies by language | Keep generated outputs inside each example's language/runtime directories and reuse dependency manifests and harnesses across example families. See [development](development.md#generated-runtime-checks). |
| Generate example outputs before verification | Example sources and handwritten harnesses remain tracked; generated files can be absent on checkout. Make targets and CI generate before runtime builds; checks reject drift in any tracked example outputs. See [development](development.md#generated-runtime-checks). |
| Shared macro processing before generators | Language count must not multiply SQL semantic work. A separate compiler package is optional; macros are still planned. See [roadmap](roadmap.md). |
| Offline generation by default | Database-assisted analysis is deferred until a concrete need defines the API and semantics. See [roadmap](roadmap.md). |
| Sequential local-ydb validation per host | Concurrent images, runtime suites and container builds have exceeded available memory. See [development](development.md). |
| User guide on ydb.tech near release | Keep technical references and examples here; publish the consumer journey with verified upstream recipes and explicit limits on the YDB site. See [release plan](release-plan.md). |
| Consumer acceptance in 0.x before 1.0.0 | The user arranges SDK reviews, production-query corpus access and real-project pilots; implementation work addresses the resulting findings. See [release plan](release-plan.md). |
| Manual publication from accumulated changelog entries | The maintainer chooses the version part in the Actions form. The workflow assigns the version and checks all artifacts before pushing the release commit/tag; RCs preserve pending notes. See [releasing](releasing.md). |
| Final repository and module name | Use `ydb-platform/sqlc-ydb` and `github.com/ydb-platform/sqlc-ydb` throughout source, release tooling and documentation. |
| Independent versions without an upstream version label | Product differences make a version-level compatibility claim unhelpful. Maintain the feature table; do not add an upstream reference to release notes or CLI metadata. |

SDK-specific behavior should be reviewed with the SDK maintainers when its public
contract is unclear. They are available within the product team; invented fallback
behavior is not a substitute for establishing that contract.
