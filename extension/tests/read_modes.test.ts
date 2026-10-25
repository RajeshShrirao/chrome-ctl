import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import {
  MAX_READ_TABLES,
  extractLinks,
  extractTables,
} from "../src/content/read.ts";

function makeDoc(html: string): Document {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${html}</body></html>`, {
    url: "https://example.test/articles/page",
  });
  return dom.window.document;
}

test("tables use thead th as headers and exclude the header row", () => {
  const doc = makeDoc(`
    <table>
      <caption>Revenue</caption>
      <thead><tr><th>Region</th><th>Q1</th></tr></thead>
      <tbody>
        <tr><td>North</td><td>10</td></tr>
        <tr><td>South</td><td>20</td></tr>
      </tbody>
    </table>`);

  const { tables, truncated } = extractTables(doc);
  assert.equal(truncated, false);
  assert.equal(tables.length, 1);
  assert.equal(tables[0]?.index, 1);
  assert.equal(tables[0]?.caption, "Revenue");
  assert.deepEqual(tables[0]?.headers, ["Region", "Q1"]);
  assert.deepEqual(tables[0]?.rows, [
    ["North", "10"],
    ["South", "20"],
  ]);
});

test("tables without th promote the first row and do not repeat it", () => {
  const doc = makeDoc(`
    <table>
      <tr><td>Name</td><td>Score</td></tr>
      <tr><td>Ada</td><td>99</td></tr>
    </table>`);

  const { tables } = extractTables(doc);
  assert.deepEqual(tables[0]?.headers, ["Name", "Score"]);
  assert.deepEqual(tables[0]?.rows, [["Ada", "99"]]);
});

test("tables with a th row inside tbody use it as the header", () => {
  const doc = makeDoc(`
    <table>
      <tr><th>Key</th><th>Value</th></tr>
      <tr><td>a</td><td>1</td></tr>
    </table>`);

  const { tables } = extractTables(doc);
  assert.deepEqual(tables[0]?.headers, ["Key", "Value"]);
  assert.deepEqual(tables[0]?.rows, [["a", "1"]]);
});

test("nested tables do not leak rows into the outer table", () => {
  const doc = makeDoc(`
    <table id="outer">
      <tr><th>A</th></tr>
      <tr><td>1</td></tr>
      <tr><td><table><tr><td>inner</td></tr></table></td></tr>
    </table>`);

  const { tables } = extractTables(doc);
  assert.deepEqual(tables[0]?.rows, [["1"], ["inner"]]);
  assert.equal(tables.length, 2);
});

test("table extraction respects maxTables and reports truncation", () => {
  const rows = Array.from({ length: MAX_READ_TABLES + 3 }, (_, index) => {
    return `<table><tr><th>T${index}</th></tr><tr><td>x</td></tr></table>`;
  }).join("");

  const doc = makeDoc(rows);
  const { tables, truncated } = extractTables(doc, undefined, MAX_READ_TABLES);
  assert.equal(tables.length, MAX_READ_TABLES);
  assert.equal(truncated, true);
});

test("links are absolute, deduped, and skip javascript hrefs", () => {
  const doc = makeDoc(`
    <a href="/one">First</a>
    <a href="https://example.test/one">Duplicate</a>
    <a href="/two">  Second  </a>
    <a href="javascript:void(0)">Ignore</a>
    <a href="#local">Skip fragment only</a>
    <a href="/icon"><img alt="Icon"></a>`);

  const { links, truncated } = extractLinks(doc);
  assert.equal(truncated, false);
  assert.deepEqual(links, [
    { text: "First", href: "https://example.test/one" },
    { text: "Second", href: "https://example.test/two" },
    { text: "Icon", href: "https://example.test/icon" },
  ]);
});

test("link extraction caps the result", () => {
  const anchors = Array.from(
    { length: 5 },
    (_, index) => `<a href="/link/${index}">L${index}</a>`,
  ).join("");
  const doc = makeDoc(anchors);

  const { links, truncated } = extractLinks(doc, undefined, 3);
  assert.equal(links.length, 3);
  assert.equal(truncated, true);
});
