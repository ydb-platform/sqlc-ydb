import assert from "node:assert/strict";

const modules = await Promise.all([
  import("../.typescript-build/authors/typescript/native/queries.js"),
  import("../.typescript-build/batch/typescript/native/queries.js"),
  import("../.typescript-build/booktest/typescript/native/queries.js"),
  import("../.typescript-build/jets/typescript/native/queries.js"),
  import("../.typescript-build/ondeck/typescript/native/queries.js"),
]);

for (const generated of modules) {
  assert.equal(typeof generated.Queries, "function");
  assert.ok(Object.entries(generated).some(([name, value]) => name.endsWith("_SQL") && typeof value === "string"));
}

function recordingClient(resultSets = [[]]) {
  const calls = [];
  const client = (text) => {
    const call = { text, parameters: new Map() };
    calls.push(call);
    const pending = Promise.resolve(resultSets);
    pending.parameter = (name, value) => {
      call.parameters.set(name, value);
      return pending;
    };
    pending.raw = () => pending;
    return pending;
  };
  return { calls, client };
}

const authorsProbe = recordingClient();
await new modules[0].Queries(authorsProbe.client).upsertAuthor({ authorId: 18446744073709551615n, authorName: "Ada", biography: null });
assert.equal(authorsProbe.calls[0].parameters.get("author_id").constructor.name, "Uint64");
assert.equal(authorsProbe.calls[0].parameters.get("biography").type.encode().type.case, "optionalType");
assert.equal(authorsProbe.calls[0].parameters.get("biography").encode().value.case, "nullFlagValue");

const batchProbe = recordingClient();
await new modules[1].Queries(batchProbe.client).createBook({
  bookId: 1n,
  authorId: 2n,
  isbn: "isbn",
  bookType: "novel",
  title: "title",
  year: 2026,
  available: 1788957296789123n,
  tags: '{"nested":[true,9007199254740993123456789,"x"]}',
});
assert.equal(batchProbe.calls[0].parameters.get("available").constructor.name, "Primitive");
assert.equal(batchProbe.calls[0].parameters.get("available").type.constructor.name, "TimestampType");
assert.equal(batchProbe.calls[0].parameters.get("available").encode().value.value, 1788957296789123n);
assert.equal(batchProbe.calls[0].parameters.get("tags").constructor.name, "Json");
assert.equal(batchProbe.calls[0].parameters.get("tags").value, '{"nested":[true,9007199254740993123456789,"x"]}');

const raw = (caseName, value) => ({ value: { case: caseName, value } });
const rawBookProbe = recordingClient([[
  {
    book_id: raw("uint64Value", 1n),
    author_id: raw("uint64Value", 2n),
    isbn: raw("textValue", "isbn"),
    book_type: raw("textValue", "novel"),
    title: raw("textValue", "title"),
    year: raw("int32Value", 2026),
    available: raw("uint64Value", 1788957296789123n),
    tags: raw("textValue", '{"number":9007199254740993123456789}'),
  },
]]);
const [rawBook] = await new modules[1].Queries(rawBookProbe.client).booksByYear(2026);
assert.equal(rawBook.available, 1788957296789123n);
assert.equal(rawBook.tags, '{"number":9007199254740993123456789}');

await assert.rejects(
  new modules[0].Queries(authorsProbe.client).getAuthor(18446744073709551616n),
  /outside YQL Uint64 range/,
);

console.log("Imported generated TypeScript for all five examples against the pinned YDB SDK.");
