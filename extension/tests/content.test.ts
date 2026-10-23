import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import { computeViewport, snapshot } from "../src/content/dom.ts";
import type { WindowLike } from "../src/content/dom.ts";
import {
  extractMarkdown,
  pickReadRoot,
  resolveReadElement,
  truncateMarkdown,
} from "../src/content/read.ts";

interface FakeRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

const VIEW: WindowLike = { innerHeight: 800, innerWidth: 1200, scrollX: 0, scrollY: 0 };

function makeDoc(html: string): Document {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${html}</body></html>`);
  return dom.window.document;
}

function stubRect(el: Element, rect: FakeRect): void {
  Object.defineProperty(el, "getBoundingClientRect", {
    configurable: true,
    value: () => ({
      x: rect.x,
      y: rect.y,
      left: rect.x,
      top: rect.y,
      right: rect.x + rect.w,
      bottom: rect.y + rect.h,
      width: rect.w,
      height: rect.h,
      toJSON: () => ({}),
    }),
  });
}

let nextY = 10;

function layout(doc: Document, selector: string, size: { w: number; h: number } = { w: 100, h: 30 }): void {
  for (const el of Array.from(doc.querySelectorAll(selector))) {
    stubRect(el, { x: 10, y: nextY, w: size.w, h: size.h });
    nextY += 40;
  }
}

function resetLayout(): void {
  nextY = 10;
}

test("type tokens and label precedence", () => {
  resetLayout();
  const doc = makeDoc(`
    <span id="lbl">Full name</span>
    <a href="/home" aria-label="Home">Home link</a>
    <input type="search" placeholder="Search" title="Search title">
    <input aria-labelledby="lbl" title="Title fallback">
    <label for="email">Email address</label>
    <input id="email" title="Ignored title">
    <label>Wrapped <input id="wrapped"></label>
    <input type="image" alt="Search icon">
    <button>Plain</button>
    <button type="submit">Send</button>
    <div role="textbox" contenteditable="true" aria-label="Message"></div>
  `);
  layout(doc, "a, input, button, div[contenteditable]");
  const snap = snapshot(doc, { all: true }, VIEW);
  const byId = (id: number) => snap.elements[id - 1];

  assert.equal(byId(1).tag, "a");
  assert.equal(byId(1).type, "link");
  assert.equal(byId(1).label, "Home");
  assert.match(snap.lines[0], /^\[1\] <a:link> "Home" @10,10$/);

  assert.equal(byId(2).type, "search");
  assert.equal(byId(2).label, "Search");
  assert.equal(byId(2).value, "");

  assert.equal(byId(3).type, "text");
  assert.equal(byId(3).label, "Full name");

  assert.equal(byId(4).label, "Email address");
  assert.equal(byId(5).label, "Wrapped");

  assert.equal(byId(6).label, "Search icon");

  assert.equal(byId(7).tag, "button");
  assert.equal(byId(7).type, "");
  assert.equal(byId(8).type, "submit");

  assert.equal(byId(9).tag, "div");
  assert.equal(byId(9).type, "textbox");
  assert.equal(byId(9).label, "Message");
});

test("form fields always render empty parentheses, other elements only when valued", () => {
  resetLayout();
  const doc = makeDoc(`
    <input type="text" aria-label="Name">
    <textarea aria-label="Bio"></textarea>
    <button aria-label="Go"></button>
  `);
  layout(doc, "input, textarea, button");

  const snap = snapshot(doc, { all: true }, VIEW);
  assert.equal(snap.lines[0], '[1] <input:text> "Name" () @10,10');
  assert.equal(snap.lines[1], '[2] <textarea> "Bio" () @10,50');
  assert.equal(snap.lines[2], '[3] <button> "Go" @10,90');
});

test("labels truncate at 80 chars and quotes are escaped in lines", () => {
  resetLayout();
  const long = "x".repeat(100);
  const doc = makeDoc(`
    <button aria-label="${long}">A</button>
    <button aria-label='Say "hi"'>B</button>
    <input aria-label="Value test" value="${"v".repeat(50)}">
  `);
  layout(doc, "button, input");

  const snap = snapshot(doc, { all: true }, VIEW);
  assert.equal(snap.elements[0].label.length, 81);
  assert.ok(snap.elements[0].label.endsWith("\u2026"));
  assert.ok(snap.lines[1].includes('\\"hi\\"'));
  assert.ok(!snap.lines[1].includes('"Say "hi""'));
  assert.equal(snap.elements[2].value.length, 40);
  assert.equal(snap.elements[2].value, "v".repeat(40));
});

test("hierarchy flattening keeps the outermost candidate", () => {
  resetLayout();
  const doc = makeDoc(`
    <button aria-label="Outer"><span role="button" aria-label="Inner">x</span></button>
  `);
  layout(doc, "button, span[role=button]");

  const snap = snapshot(doc, { all: true }, VIEW);
  assert.equal(snap.elements.length, 1);
  assert.equal(snap.elements[0].tag, "button");
  assert.equal(snap.elements[0].label, "Outer");
  assert.equal(snap.elements[0].id, 1);
});

test("visibility and 4px culling", () => {
  resetLayout();
  const doc = makeDoc(`
    <button id="display" style="display:none">A</button>
    <button id="vis" style="visibility:hidden">B</button>
    <button id="collapse" style="visibility:collapse">C</button>
    <button id="op" style="opacity:0">D</button>
    <button id="tiny">E</button>
    <button id="ok">F</button>
  `);
  layout(doc, "button");
  stubRect(doc.getElementById("tiny")!, { x: 10, y: 10, w: 3, h: 20 });

  const snap = snapshot(doc, { all: true }, VIEW);
  assert.deepEqual(
    snap.elements.map((element) => element.label),
    ["F"],
  );
});

test("default viewport filtering uses a 100px margin, all disables it", () => {
  resetLayout();
  const doc = makeDoc(`
    <button id="far-above">A</button>
    <button id="near-above">B</button>
    <button id="near-below">C</button>
    <button id="far-below">D</button>
  `);
  stubRect(doc.getElementById("far-above")!, { x: 10, y: -150, w: 100, h: 20 });
  stubRect(doc.getElementById("near-above")!, { x: 10, y: -50, w: 100, h: 20 });
  stubRect(doc.getElementById("near-below")!, { x: 10, y: 850, w: 100, h: 20 });
  stubRect(doc.getElementById("far-below")!, { x: 10, y: 950, w: 100, h: 20 });

  const filtered = snapshot(doc, {}, VIEW);
  assert.deepEqual(
    filtered.elements.map((element) => element.label),
    ["B", "C"],
  );

  const all = snapshot(doc, { all: true }, VIEW);
  assert.deepEqual(
    all.elements.map((element) => element.label),
    ["A", "B", "C", "D"],
  );
});

test("formsOnly keeps form fields and submit-like buttons", () => {
  resetLayout();
  const doc = makeDoc(`
    <a href="/x">Link</a>
    <form>
      <input type="text" aria-label="Name">
      <input type="hidden" name="h" value="1">
      <button>Submit</button>
      <button type="button">Cancel</button>
    </form>
    <textarea aria-label="Bio"></textarea>
    <select aria-label="Choice"><option>A</option></select>
    <button type="submit" aria-label="Outside submit"></button>
    <div contenteditable="true" aria-label="Editor"></div>
  `);
  layout(doc, "a, input, button, textarea, select, div[contenteditable]");

  const snap = snapshot(doc, { all: true, formsOnly: true }, VIEW);
  assert.deepEqual(
    snap.elements.map((element) => element.label),
    ["Name", "Submit", "Bio", "Choice", "Outside submit", "Editor"],
  );
});

test("emitted ids stay contiguous when candidates are culled", () => {
  resetLayout();
  const doc = makeDoc(`
    <button>A</button>
    <button id="tiny">B</button>
    <button>C</button>
  `);
  layout(doc, "button");
  stubRect(doc.getElementById("tiny")!, { x: 10, y: 50, w: 3, h: 30 });

  const snap = snapshot(doc, { all: true }, VIEW);
  assert.deepEqual(
    snap.elements.map((element) => [element.id, element.label]),
    [
      [1, "A"],
      [2, "C"],
    ],
  );
  assert.equal(snap.byId.get(1)?.textContent, "A");
  assert.equal(snap.byId.has(3), false);
});

test("viewport math clamps and handles zero height", () => {
  const doc = makeDoc("<p>hi</p>");
  Object.defineProperty(doc.documentElement, "scrollHeight", { value: 3000 });

  const viewport = computeViewport(doc, { innerHeight: 800, scrollY: 100 });
  assert.deepEqual(viewport, {
    scrollY: 100,
    viewportHeight: 800,
    totalHeight: 3000,
    percent: 30,
  });

  const empty = computeViewport(makeDoc("<p>hi</p>"));
  assert.equal(empty.totalHeight, 0);
  assert.equal(empty.percent, 0);
});

test("read extracts headings, links, emphasis, lists, and strips hidden nodes", () => {
  const doc = makeDoc(`
    <h1>Title</h1>
    <p>Hello <a href="/x">world</a> and <strong>bold</strong> <em>em</em> <code>code</code>.</p>
    <ul>
      <li>One</li>
      <li>Two
        <ul><li>Nested</li></ul>
      </li>
    </ul>
    <script>bad()</script>
    <style>.x { color: red; }</style>
    <div style="display:none">Hidden text</div>
    <hr>
  `);

  const markdown = extractMarkdown(doc);
  assert.ok(markdown.includes("# Title"));
  assert.ok(markdown.includes("Hello [world](/x) and **bold** *em* `code`."));
  assert.ok(markdown.includes("- One\n- Two\n  - Nested"));
  assert.ok(markdown.includes("---"));
  assert.ok(!markdown.includes("Hidden text"));
  assert.ok(!markdown.includes("bad()"));
});

test("read targets a selector and the densest main container", () => {
  const doc = makeDoc(`
    <div id="target"><h2>Target</h2><p>Body copy</p></div>
    <main>short</main>
    <article>${"long text ".repeat(20)}</article>
  `);

  const target = resolveReadElement(doc, undefined, undefined, "#target");
  assert.equal(target.id, "target");
  assert.equal(extractMarkdown(doc, target), "## Target\n\nBody copy");

  assert.equal((pickReadRoot(doc) as Element).tagName, "ARTICLE");
});

test("read resolves snapshot ids and reports missing ids", () => {
  const doc = makeDoc('<button aria-label="Save">Save</button>');
  const button = doc.querySelector("button")!;
  const index = new Map<number, Element>([[1, button]]);

  assert.equal(resolveReadElement(doc, index, 1), button);
  assert.throws(
    () => resolveReadElement(doc, index, 7),
    /element id 7 not found; run snap first/,
  );
  assert.throws(
    () => resolveReadElement(doc, index, undefined, "###"),
    /invalid selector/,
  );
  assert.throws(
    () => resolveReadElement(doc, index, undefined, "#nope"),
    /selector not found: #nope/,
  );
});

test("truncateMarkdown caps output and appends the marker", () => {
  assert.equal(truncateMarkdown("abcdef", 10), "abcdef");
  assert.equal(truncateMarkdown("abcdef", 3), "abc\n\n[truncated]");
});
