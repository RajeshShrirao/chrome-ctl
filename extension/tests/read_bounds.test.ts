import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import { describeRead } from "../src/content/indexer.ts";
import { extractTables } from "../src/content/read.ts";
import { CONTENT_CHANNEL, READ_MAX_CHARS } from "../src/shared/content.ts";

function makeDoc(html: string): Document {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${html}</body></html>`, {
    url: "https://example.test/page",
  });
  return dom.window.document;
}

test("extractTables stops adding rows once maxChars budget is reached", () => {
  const doc = makeDoc(`
    <table>
      <thead><tr><th>AA</th><th>BB</th></tr></thead>
      <tbody>
        <tr><td>1111</td><td>2222</td></tr>
        <tr><td>3333</td><td>4444</td></tr>
        <tr><td>5555</td><td>6666</td></tr>
      </tbody>
    </table>
  `);

  // Header = 4 chars; each row = 8 chars. Budget 20 allows header + 2 rows (4 + 16 = 20).
  const res = extractTables(doc, undefined, 10, 20);
  assert.equal(res.truncated, true);
  assert.equal(res.tables.length, 1);
  assert.deepEqual(res.tables[0]?.headers, ["AA", "BB"]);
  assert.deepEqual(res.tables[0]?.rows, [
    ["1111", "2222"],
    ["3333", "4444"],
  ]);
});

test("extractTables stops adding subsequent tables once maxChars budget is exhausted", () => {
  const doc = makeDoc(`
    <table>
      <tr><th>H1</th></tr>
      <tr><td>12345678</td></tr>
    </table>
    <table>
      <tr><th>H2</th></tr>
      <tr><td>abcdefgh</td></tr>
    </table>
  `);

  // First table uses 2 + 8 = 10 chars; budget 10 fits table 1 cleanly but blocks table 2.
  const exact = extractTables(doc, undefined, 10, 10);
  assert.equal(exact.truncated, true);
  assert.equal(exact.tables.length, 1);
  assert.deepEqual(exact.tables[0]?.headers, ["H1"]);
  assert.deepEqual(exact.tables[0]?.rows, [["12345678"]]);

  // Budget 5 cannot even fit table 1's row after header (2 + 8 > 5), and still blocks table 2.
  const partial = extractTables(doc, undefined, 10, 5);
  assert.equal(partial.truncated, true);
  assert.equal(partial.tables.length, 1);
  assert.deepEqual(partial.tables[0]?.rows, []);

  // Budget 1 cannot fit table 1's header (2 > 1).
  const tiny = extractTables(doc, undefined, 10, 1);
  assert.equal(tiny.truncated, true);
  assert.equal(tiny.tables.length, 0);
});

test("extractTables defaults to 100_000 char budget when maxChars is omitted", () => {
  const doc = makeDoc(`
    <table>
      <tr><th>Col</th></tr>
      <tr><td>${"x".repeat(500)}</td></tr>
    </table>
  `);
  const res = extractTables(doc);
  assert.equal(res.truncated, false);
  assert.equal(res.tables.length, 1);
  assert.equal(res.tables[0]?.rows[0]?.[0], "x".repeat(500));
});

test("describeRead enforces request.maxChars in markdown mode and sets truncated", () => {
  const doc = makeDoc(`<article><p>${"a".repeat(200)}</p></article>`);

  const short = describeRead(
    { channel: CONTENT_CHANNEL, type: "read", maxChars: 50 },
    doc,
  );
  assert.equal(short.ok, true);
  assert.equal(short.type, "read");
  if (short.ok && short.type === "read") {
    assert.equal(short.markdown, `${"a".repeat(50)}\n\n[truncated]`);
    assert.equal(short.truncated, true);
  }

  const fits = describeRead(
    { channel: CONTENT_CHANNEL, type: "read", maxChars: 200 },
    doc,
  );
  assert.equal(fits.ok, true);
  if (fits.ok && fits.type === "read") {
    assert.equal(fits.markdown, "a".repeat(200));
    assert.equal(fits.truncated, undefined);
  }
});

test("describeRead clamps maxChars to [1, READ_MAX_CHARS] and defaults to READ_MAX_CHARS", () => {
  const smallDoc = makeDoc("<p>Hello world</p>");
  const clampedMin = describeRead(
    { channel: CONTENT_CHANNEL, type: "read", maxChars: 0 },
    smallDoc,
  );
  assert.equal(clampedMin.ok, true);
  if (clampedMin.ok && clampedMin.type === "read") {
    assert.equal(clampedMin.markdown, "H\n\n[truncated]");
    assert.equal(clampedMin.truncated, true);
  }

  const hugeDoc = makeDoc(`<p>${"z".repeat(READ_MAX_CHARS + 50)}</p>`);
  const clampedMax = describeRead(
    { channel: CONTENT_CHANNEL, type: "read", maxChars: READ_MAX_CHARS + 9999 },
    hugeDoc,
  );
  assert.equal(clampedMax.ok, true);
  if (clampedMax.ok && clampedMax.type === "read") {
    assert.equal(clampedMax.markdown, `${"z".repeat(READ_MAX_CHARS)}\n\n[truncated]`);
    assert.equal(clampedMax.truncated, true);
  }

  const omittedDefault = describeRead({ channel: CONTENT_CHANNEL, type: "read" }, hugeDoc);
  assert.equal(omittedDefault.ok, true);
  if (omittedDefault.ok && omittedDefault.type === "read") {
    assert.equal(omittedDefault.markdown, `${"z".repeat(READ_MAX_CHARS)}\n\n[truncated]`);
    assert.equal(omittedDefault.truncated, true);
  }
});

test("describeRead forwards maxChars to extractTables in tables mode", () => {
  const doc = makeDoc(`
    <table>
      <thead><tr><th>Key</th><th>Val</th></tr></thead>
      <tbody>
        <tr><td>k1</td><td>vvvvvvvvvv</td></tr>
        <tr><td>k2</td><td>wwwwwwwwww</td></tr>
      </tbody>
    </table>
  `);

  const res = describeRead(
    { channel: CONTENT_CHANNEL, type: "read", mode: "tables", maxChars: 20 },
    doc,
  );
  assert.equal(res.ok, true);
  if (res.ok && res.type === "read") {
    assert.equal(res.truncated, true);
    assert.equal(res.tables?.length, 1);
    assert.deepEqual(res.tables?.[0]?.rows, [["k1", "vvvvvvvvvv"]]);
  }
});
