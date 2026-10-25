import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import { snapshot } from "../src/content/dom.ts";
import type { SnapOptions } from "../src/content/dom.ts";
import type { WindowLike } from "../src/content/dom.ts";

const VIEW: WindowLike = { innerHeight: 800, innerWidth: 1200, scrollX: 0, scrollY: 0 };

let nextY = 10;

function makeDoc(html: string): Document {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${html}</body></html>`);
  return dom.window.document;
}

function layoutAll(doc: Document): void {
  for (const el of Array.from(doc.querySelectorAll("*"))) {
    const top = nextY;
    nextY += 40;
    Object.defineProperty(el, "getBoundingClientRect", {
      configurable: true,
      value: () => ({
        x: 10,
        y: top,
        left: 10,
        top,
        right: 110,
        bottom: top + 30,
        width: 100,
        height: 30,
        toJSON: () => ({}),
      }),
    });
  }
}

function run(doc: Document, options: SnapOptions = {}) {
  return snapshot(doc, options, VIEW);
}

test("closed and open menus report collapsed and expanded", () => {
  nextY = 10;
  const doc = makeDoc(`
    <div role="combobox" aria-expanded="false">Search</div>
    <div role="combobox" aria-expanded="true">Filters</div>
    <details><summary>More options</summary><p>Body</p></details>
  `);
  layoutAll(doc);

  const result = run(doc);
  const byLabel = new Map(result.elements.map((el) => [el.label, el]));
  assert.equal(byLabel.get("Search")?.state, "collapsed");
  assert.equal(byLabel.get("Filters")?.state, "expanded");
  assert.equal(byLabel.get("More options")?.state, "collapsed");
});

test("hidden file inputs are emitted at their visible label", () => {
  nextY = 10;
  const doc = makeDoc(`
    <label for="report">Upload report</label>
    <input id="report" type="file" style="display: none">
  `);
  layoutAll(doc);

  const result = run(doc);
  const file = result.elements.find((el) => el.type === "file");
  assert.ok(file, "file input should be indexed");
  assert.equal(file.label, "Upload report");
  assert.match(file.state, /file/);
  assert.equal(file.x, 10);
  assert.equal(result.byId.get(file.id)?.tagName.toLowerCase(), "input");
});

test("pointer-only controls stay out of the default snapshot and are capped in deep mode", () => {
  nextY = 10;
  const html = Array.from(
    { length: 1000 },
    (_, index) => `<div style="cursor: pointer">Item ${index}</div>`,
  ).join("");
  const doc = makeDoc(html);
  layoutAll(doc);

  const shallow = run(doc, { all: true, maxElements: 50 });
  assert.equal(shallow.elements.length, 0);
  assert.equal(shallow.omitted, 0);

  const deep = run(doc, { all: true, deep: true, maxElements: 50 });
  assert.equal(deep.elements.length, 50);
  assert.equal(deep.omitted, 950);
});

test("listbox options stay individually addressable", () => {
  nextY = 10;
  const doc = makeDoc(`
    <div role="combobox" aria-expanded="true">Topic</div>
    <ul role="listbox">
      <li role="option">Browser automation</li>
      <li role="option">Token budgets</li>
    </ul>
  `);
  layoutAll(doc);

  const result = run(doc);
  const labels = result.elements.map((el) => el.label);
  assert.ok(labels.includes("Browser automation"), labels.join(" | "));
  assert.ok(labels.includes("Token budgets"), labels.join(" | "));
});

test("menu items stay individually addressable inside a menu", () => {
  nextY = 10;
  const doc = makeDoc(`
    <div role="menu">
      <div role="menuitem">Rename</div>
      <div role="menuitem">Delete</div>
    </div>
  `);
  layoutAll(doc);

  const labels = run(doc).elements.map((el) => el.label);
  assert.ok(labels.includes("Rename"));
  assert.ok(labels.includes("Delete"));
});

test("a button still flattens its inner span", () => {
  nextY = 10;
  const doc = makeDoc('<button><span>Save changes</span></button>');
  layoutAll(doc);

  const labels = run(doc).elements.map((el) => el.label);
  assert.deepEqual(labels, ["Save changes"]);
});

test("bare grid rows are opt-in", () => {
  nextY = 10;
  const doc = makeDoc(`
    <div role="grid">
      <div role="row"><div role="gridcell">Name</div></div>
    </div>
  `);
  layoutAll(doc);

  assert.equal(run(doc).elements.length, 0);
  const deep = run(doc, { deep: true });
  const roles = deep.elements.map((el) => el.label);
  assert.ok(roles.includes("Name"));
});
