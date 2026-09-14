import assert from "node:assert/strict";

const modules = await Promise.all([
  import("./.typescript-build/authors/typescript/native/queries.js"),
  import("./.typescript-build/batch/typescript/native/queries.js"),
  import("./.typescript-build/booktest/typescript/native/queries.js"),
  import("./.typescript-build/jets/typescript/native/queries.js"),
  import("./.typescript-build/ondeck/typescript/native/queries.js"),
]);

for (const generated of modules) {
  assert.equal(typeof generated.Queries, "function");
  assert.deepEqual(Object.keys(generated), ["Queries"]);
}

function recordingClient(resultSets = [[]]) {
  const calls = [];
  const client = (text) => {
    const call = { text: text.join(""), parameters: new Map() };
    calls.push(call);
    const stmt = Promise.resolve(resultSets);
    stmt.parameter = (name, value) => {
      call.parameters.set(name, value);
      return stmt;
    };
    return stmt;
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
  available: new Date(1788957296789),
  tags: '{"nested":[true,9007199254740993123456789,"x"]}',
});
assert.equal(batchProbe.calls[0].parameters.get("available").constructor.name, "Timestamp");
assert.equal(batchProbe.calls[0].parameters.get("available").type.constructor.name, "TimestampType");
assert.equal(batchProbe.calls[0].parameters.get("available").encode().value.value, 1788957296789000n);
assert.equal(batchProbe.calls[0].parameters.get("tags").constructor.name, "Json");
assert.equal(batchProbe.calls[0].parameters.get("tags").value, '{"nested":[true,9007199254740993123456789,"x"]}');

const bookProbe = recordingClient([[
  {
    book_id: 1n,
    author_id: 2n,
    isbn: "isbn",
    book_type: "novel",
    title: "title",
    year: 2026,
    available: new Date(1788957296789),
    tags: { genre: "novel" },
  },
]]);
const [book] = await new modules[1].Queries(bookProbe.client).booksByYear(2026);
assert.equal(book.available.getTime(), 1788957296789);
assert.deepEqual(book.tags, { genre: "novel" });
assert.match(bookProbe.calls[0].text, /book_id/);


const batchInput = { bookId: 18446744073709551615n, authorId: 2n, isbn: "batch", bookType: "novel", title: "batch", year: 2026, available: new Date(1788957296789), tags: '{"batch":true}' };
for (const books of [[], [batchInput], [batchInput, { ...batchInput, bookId: 3n }]]) {
  const probe = recordingClient();
  await new modules[1].Queries(probe.client).createBooks(books);
  const parameter = probe.calls[0].parameters.get("books");
  const type = parameter.type.encode().type;
  assert.equal(type.case, "listType");
  assert.equal(type.value.item.type.case, "structType");
  const names = type.value.item.type.value.members.map(member => member.name);
  const rows = parameter.encode().items;
  assert.equal(rows.length, books.length);
  for (let i = 0; i < books.length; i++) {
    assert.equal(rows[i].items[names.indexOf("book_id")].value.value, books[i].bookId);
    assert.equal(rows[i].items[names.indexOf("tags")].value.value, books[i].tags);
    assert.equal(rows[i].items[names.indexOf("available")].value.value, 1788957296789000n);
  }
}

console.log("Imported generated TypeScript for all five examples against the pinned YDB SDK.");
