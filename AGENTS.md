# Working on sqlc-ydb

Read [.agents/context.md](.agents/context.md) for the code map and links to the
current contracts. Inspect the working tree before editing; preserve unrelated
changes. Repository documentation is in English.

## Implementation rules

- Keep the pipeline in [architecture](docs/architecture.md): direct ANTLR YQL
  contexts, semantic analysis, one resolved result, built-in generators. Do not
  introduce an intermediate AST, engine registry or external plugin protocol.
- Unsupported syntax, types and options must produce actionable errors. Do not
  guess a type, substitute a default value or silently ignore an option to make
  generation succeed. Defaults and SDK compatibility branches need a documented
  contract and a test that distinguishes them from an error.
- Prefer small cohesive functions and explicit control flow. Extract duplicated
  policy only when it has the same meaning and reason to change. Runtime binding,
  decoding and transaction ownership differ across SDKs; similar-looking code is
  not sufficient reason to build a common adapter framework.
- Keep semantic SQL work before generation. Future macros must resolve once per
  compilation unit, independently of the number of generators. Driver placeholder
  rendering must preserve SQL strings, comments, identifiers and local bindings.
- Validate generated identifiers and collisions. Preserve query text exactly in
  readable multiline literals, including delimiters, Unicode and control bytes.
  Generated code must use the actual SDK row/binding contract, not guessed object
  shapes or fallback values. Make resource and transaction ownership explicit.
- Keep runtime SDK dependencies in tests/examples, out of the generator's module
  imports. Do not add local sibling `replace` directives. Verify SDK APIs against
  pinned dependencies and record source evidence in [provenance](docs/provenance.md).

## Verification

- For a bug, first add a regression that demonstrates the wrong behavior. Assert
  meaningful results or diagnostics, not the implementation's internal steps.
- Run the affected package tests while editing; run `make check` for the integrated
  change. Commands and prerequisites are in [development](docs/development.md).
- When output changes intentionally, regenerate examples with `make generate` and
  update [golden fixtures](internal/endtoend/README.md). Review the generated diff;
  do not hand-edit generated files or accept a baseline just to make tests pass.
- For SDK binding, row decoding or transaction changes, compile generated code
  against the pinned SDK and run the relevant execution test. Rendering tests and
  mocks alone do not establish SDK compatibility. SQL literal tests must evaluate
  generated literals with the language's compiler/runtime and compare the original bytes.
- Run local-ydb images, live runtime suites and Docker builds **sequentially per
  host**. Keep `go test -p 1` for live suites and no `t.Parallel` in them. Use an
  isolated disposable database; never stop unrelated containers. Prefer Linux CI
  for heavy acceptance checks on memory-constrained development machines.

## Documentation and project memory

- User documentation is planned for ydb.tech near release. Keep technical
  compatibility contracts and SDK references here; once the site guide exists,
  link it rather than maintaining two full user guides. Contributor commands
  belong in [development](docs/development.md). Update the canonical page when
  behavior changes, then link to it elsewhere.
- Release preparation is tracked in [the release plan](docs/release-plan.md).
  Keep SDK maintainer reviews and consumer pilots assigned to the user. Preparing
  artifacts or changelog entries does not authorize creating a tag or publishing.
  Accumulate consumer-facing changes under `Unreleased`; the manual publish
  workflow assigns version headings. The first release describes capabilities,
  not fixes to development iterations that were never released.
- Remove dead branches, obsolete plans and comments that merely narrate code.
  Keep comments that explain a constraint or non-obvious SDK behavior. Do not add
  speculative abstractions or LLM task assignments to production documentation.
- `.agents/` is a small maintained project memory: code map, lasting decisions and
  pointers. Update it when boundaries or decisions change. Do not copy reference
  docs, generated APIs, dependency versions, machine paths or transient CI state
  into it. Mark planned work as planned; tests and source establish what exists.
- When adapting upstream source or tests, preserve license notices and record the
  exact source commit. This is an independent implementation, not a source fork
  requiring upstream merges.
