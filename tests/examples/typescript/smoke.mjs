import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { Driver } from "@ydbjs/core";
import { query } from "@ydbjs/query";

import { Queries as AuthorQueries } from "./.typescript-build/authors/typescript/native/queries.js";
import { Queries as BatchQueries } from "./.typescript-build/batch/typescript/native/queries.js";
import { Queries as BooktestQueries } from "./.typescript-build/booktest/typescript/native/queries.js";
import { Queries as JetsQueries } from "./.typescript-build/jets/typescript/native/queries.js";
import { Queries as OndeckQueries } from "./.typescript-build/ondeck/typescript/native/queries.js";

const dsn = process.env.YDB_CONNECTION_STRING;
if (!dsn) {
  throw new Error("YDB_CONNECTION_STRING is required, for example grpc://localhost:2136/local");
}

const driver = new Driver(dsn);
const client = query(driver);

async function source(relative) {
  return readFile(new URL(relative, import.meta.url), "utf8");
}

async function executeFile(relative) {
  await client(await source(relative));
}

async function dropTables(names) {
  for (const name of names) {
    await client(`DROP TABLE ${name};`);
  }
}

async function runAuthors() {
  await executeFile("../../../examples/authors/schema.sql");
  try {
    const queries = new AuthorQueries(client);
    const id = 18446744073709551615n;
    assert.equal(await queries.getAuthor(id), null);
    assert.deepEqual(await queries.createAuthor({ authorId: id, authorName: "Ada", biography: null }), { id, name: "Ada", bio: null });
    assert.deepEqual(await queries.getAuthorName(id), { name: "Ada" });
    assert.equal((await queries.listAuthors())[0].id, id);
    await queries.upsertAuthor({ authorId: id, authorName: "Ada Lovelace", biography: "programmer" });
    assert.equal((await queries.getAuthor(id)).bio, "programmer");
    await queries.deleteAuthor(id);
    assert.equal(await queries.getAuthor(id), null);
    const aborted = new Error("rollback generated calls");
    await assert.rejects(client.transaction(async (tx, signal) => {
      const transactional = new AuthorQueries(tx);
      const configure = (stmt) => { stmt.signal(signal); };
      await transactional.upsertAuthor({ authorId: id, authorName: "transaction", biography: null }, configure);
      assert.equal((await transactional.getAuthor(id, configure)).name, "transaction");
      throw aborted;
    }), (error) => error.cause === aborted);
    assert.equal(await queries.getAuthor(id), null);
  } finally {
    await dropTables(["authors"]);
  }
}

async function runBatch() {
  await executeFile("../../../examples/batch/schema.sql");
  try {
    const queries = new BatchQueries(client);
    const authorId = 18446744073709551615n;
    const bookId = 18446744073709551614n;
    assert.deepEqual(await queries.createAuthor({ authorId, name: "Octavia", biography: '{"born":1947}' }), { author_id: authorId, name: "Octavia", biography: { born: 1947 } });
    const available = new Date(1788957296789);
    const book = await queries.createBook({ bookId, authorId, isbn: "978-0", bookType: "novel", title: "Kindred", year: 1979, available, tags: '["history","science-fiction"]' });
    assert.equal(book.book_id, bookId);
    assert.deepEqual(book.available, available);
    assert.deepEqual(book.tags, ["history", "science-fiction"]);
    assert.equal((await queries.booksByYear(1979))[0].author_id, authorId);
    await queries.updateBook({ title: "Kindred (updated)", tags: '{"shelf":"read"}', bookId });
    assert.deepEqual((await queries.getBiography(authorId)).biography, { born: 1947 });
    await queries.deleteBook(bookId);
    await queries.deleteBookExecResult(bookId);
    await queries.deleteBookNamedFunc(bookId);
    await queries.deleteBookNamedSign(bookId);
    assert.equal((await queries.getAuthor(authorId)).author_id, authorId);
  } finally {
    await dropTables(["books", "authors"]);
  }
}

async function runBooktest() {
  await executeFile("../../../examples/booktest/schema.sql");
  try {
    const queries = new BooktestQueries(client);
    const authorId = 91n;
    const bookId = 92n;
    await queries.createAuthor({ authorId, name: "Ursula" });
    const available = new Date(1735787045678);
    await queries.createBook({ bookId, authorId, isbn: "isbn", bookType: "novel", title: "Earthsea", publicationYear: 1968, available, tags: '["fantasy"]' });
    assert.equal((await queries.getAuthor(authorId)).name, "Ursula");
    assert.deepEqual((await queries.getBook(bookId)).available, available);
    assert.equal((await queries.booksByTitleYear({ title: "Earthsea", publicationYear: 1968 }))[0].book_id, bookId);
    assert.equal((await queries.booksByTags('["fantasy"]'))[0]["a.name"], "Ursula");
    assert.deepEqual(await queries.sayHello("YDB"), { greeting: "hello YDB" });
    await queries.updateBook({ bookId, title: "A Wizard of Earthsea", tags: '["classic"]' });
    await queries.updateBookISBN({ bookId, title: "A Wizard of Earthsea", tags: '["classic"]', isbn: "new-isbn" });
    await queries.deleteAuthorBeforeYear({ authorId, publicationYear: 1900 });
    await queries.deleteBook(bookId);
    assert.equal(await queries.getBook(bookId), null);
  } finally {
    await dropTables(["books", "authors"]);
  }
}

async function runJets() {
  await executeFile("../../../examples/jets/schema.sql");
  try {
    const queries = new JetsQueries(client);
    assert.deepEqual(await queries.countPilots(), { pilot_count: 0n });
    await client('UPSERT INTO pilots (id, name) VALUES (1, "Amelia"u), (2, "Bessie"u);');
    assert.deepEqual(await queries.listPilots(), [{ id: 1, name: "Amelia" }, { id: 2, name: "Bessie" }]);
    await queries.deletePilot(1);
    assert.equal((await queries.countPilots()).pilot_count, 1n);
  } finally {
    await dropTables(["pilot_languages", "languages", "jets", "pilots"]);
  }
}

async function runOndeck() {
  for (const migration of ["0001_city.sql", "0002_venue.sql", "0003_rename_venue.sql", "0004_add_created_at.sql", "0005_drop_column.sql"]) {
    await executeFile(`../../../examples/ondeck/schema/${migration}`);
  }
  try {
    const queries = new OndeckQueries(client);
    assert.deepEqual(await queries.createCity({ name: "London", slug: "london" }), { slug: "london", name: "London" });
    assert.deepEqual(await queries.getCity("london"), { slug: "london", name: "London" });
    await queries.updateCityName({ name: "Greater London", slug: "london" });
    assert.equal((await queries.listCities())[0].name, "Greater London");
    const createdAt = new Date(1788948672345);
    const venue = await queries.createVenue({ id: 7n, slug: "roundhouse", name: "Roundhouse", city: "london", createdAt, spotifyPlaylist: "spotify:playlist:1", status: "open", statuses: '["open"]', tags: '{"genre":"rock"}' });
    assert.deepEqual(venue, { id: 7n });
    assert.deepEqual((await queries.getVenue({ slug: "roundhouse", city: "london" })).created_at, createdAt);
    assert.equal((await queries.listVenues("london"))[0].id, 7n);
    assert.deepEqual(await queries.venueCountByCity(), [{ city: "london", venue_count: 1n }]);
    assert.deepEqual(await queries.updateVenueName({ name: "The Roundhouse", slug: "roundhouse" }), { id: 7n });
    await queries.deleteVenue("roundhouse");
    assert.equal(await queries.getVenue({ slug: "roundhouse", city: "london" }), null);
  } finally {
    await dropTables(["venue", "city"]);
  }
}

try {
  await driver.ready();
  await runAuthors();
  await runBatch();
  await runBooktest();
  await runJets();
  await runOndeck();
  console.log("All TypeScript generated-query examples passed.");
} finally {
  await client[Symbol.asyncDispose]();
  await driver[Symbol.asyncDispose]();
}
