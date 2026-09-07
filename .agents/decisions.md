# Decisions

These choices constrain maintenance; implementation details remain in the linked
documents. Revisit a decision explicitly rather than letting a local workaround
change the architecture.

| Decision | Reason and reference |
| --- | --- |
| Independent YDB-only implementation | Compatibility concerns user workflow, not upstream internal code or Git history. See [compatibility](../docs/compatibility.md). |
| Keep semantic analysis; no intermediate AST | Direct ANTLR contexts avoid a second syntax representation while resolved types remain necessary for code generation. See [architecture](../docs/architecture.md). |
| Built-in generators only | New language support belongs in this repository; external engine/codegen/WASM/process plugins are deliberately excluded. See [compatibility](../docs/compatibility.md). |
| One modern C# ADO.NET target | The official SDK exposes ADO.NET already; a second nominally native profile would duplicate it. See [C#](../docs/csharp.md). |
| SQL-first Java framework adapters | Typed query methods and projection records fit JdbcTemplate and Hibernate JDBC callbacks. Do not infer ORM entities from arbitrary SQL. See [Java](../docs/java.md). |
| Shared macro processing before generators | Language count must not multiply SQL semantic work. A separate compiler package is optional; macros are still planned. See [roadmap](../docs/roadmap.md). |
| Offline generation by default | Database-assisted analysis is deferred until a concrete need defines the API and semantics. See [roadmap](../docs/roadmap.md). |
| Sequential local-ydb validation per host | Concurrent images, runtime suites and container builds have exceeded available memory. See [development](../docs/development.md). |
| User guide on ydb.tech near release | Keep technical references and examples here; publish the consumer journey with verified upstream recipes and explicit limits on the YDB site. See [release plan](../docs/release-plan.md). |
| Consumer acceptance in 0.x before 1.0.0 | The user arranges SDK reviews, production-query corpus access and real-project pilots; implementation work addresses the resulting findings. See [release plan](../docs/release-plan.md). |
| Manual publication from accumulated changelog entries | The maintainer chooses the version part in the Actions form. The workflow assigns the version and checks all artifacts before pushing the release commit/tag; RCs preserve pending notes. See [releasing](../docs/releasing.md). |

SDK-specific behavior should be reviewed with the SDK maintainers when its public
contract is unclear. They are available within the product team; invented fallback
behavior is not a substitute for establishing that contract.
