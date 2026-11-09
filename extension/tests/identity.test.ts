import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import { snapshot } from "../src/content/dom.ts";
import type { WindowLike } from "../src/content/dom.ts";
import {
  applyIdentity,
  elementFingerprint,
  emptyIdentity,
  fingerprintHash,
} from "../src/content/identity.ts";
import type { IdentityResult, IdentityState } from "../src/content/identity.ts";

const VIEW: WindowLike = { innerHeight: 800, innerWidth: 1200, scrollX: 0, scrollY: 0 };

interface FakeRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

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

function run(
  doc: Document,
  state: IdentityState,
  snapshotId: number,
): IdentityResult {
  let startId = 1;
  for (const id of state.records.keys()) {
    if (id >= startId) {
      startId = id + 1;
    }
  }
  const fresh = snapshot(doc, { all: true, startId }, VIEW);
  return applyIdentity(fresh.elements, fresh.byId, state, snapshotId);
}

function ids(result: IdentityResult): number[] {
  return result.elements.map((element) => element.id);
}

function changes(result: IdentityResult): Array<string | undefined> {
  return result.elements.map((element) => element.change);
}

test("fingerprint hash is deterministic and distinguishes inputs", () => {
  assert.equal(fingerprintHash("same input"), fingerprintHash("same input"));
  assert.notEqual(fingerprintHash("a"), fingerprintHash("b"));
  assert.match(fingerprintHash("a"), /^[0-9a-f]{16}$/);
});

test("same DOM nodes keep ids and unchanged geometry reports same", () => {
  const doc = makeDoc(`
    <button id="a">Alpha</button>
    <button id="b">Beta</button>
  `);
  stubRect(doc.getElementById("a")!, { x: 10, y: 10, w: 80, h: 30 });
  stubRect(doc.getElementById("b")!, { x: 10, y: 50, w: 80, h: 30 });

  const first = run(doc, emptyIdentity(), 1);
  assert.deepEqual(ids(first), [1, 2]);
  assert.deepEqual(changes(first), ["added", "added"]);
  assert.deepEqual(first.removed, []);

  const second = run(doc, first.state, 2);
  assert.deepEqual(ids(second), [1, 2]);
  assert.deepEqual(changes(second), ["same", "same"]);
});

test("moved and updated elements are classified without changing ids", () => {
  const doc = makeDoc(`
    <button id="a">Alpha</button>
    <button id="b">Beta</button>
  `);
  stubRect(doc.getElementById("a")!, { x: 10, y: 10, w: 80, h: 30 });
  stubRect(doc.getElementById("b")!, { x: 10, y: 50, w: 80, h: 30 });

  const first = run(doc, emptyIdentity(), 1);

  stubRect(doc.getElementById("b")!, { x: 10, y: 200, w: 80, h: 30 });
  const second = run(doc, first.state, 2);
  assert.deepEqual(ids(second), [1, 2]);
  assert.deepEqual(changes(second), ["same", "moved"]);

  doc.getElementById("a")!.textContent = "Alpha renamed";
  const third = run(doc, second.state, 3);
  assert.deepEqual(ids(third), [1, 2]);
  assert.deepEqual(changes(third), ["updated", "same"]);
});

test("shuffling sibling order keeps ids for uniquely identified elements", () => {
  const doc = makeDoc(`
    <button id="a">Alpha</button>
    <button id="b">Beta</button>
    <a id="c" href="/next">Gamma</a>
  `);
  stubRect(doc.getElementById("a")!, { x: 10, y: 10, w: 80, h: 30 });
  stubRect(doc.getElementById("b")!, { x: 10, y: 50, w: 80, h: 30 });
  stubRect(doc.getElementById("c")!, { x: 10, y: 90, w: 80, h: 30 });

  const first = run(doc, emptyIdentity(), 1);
  assert.deepEqual(ids(first), [1, 2, 3]);

  const body = doc.body;
  const a = doc.getElementById("a")!;
  body.insertBefore(doc.getElementById("c")!, a);
  const second = run(doc, first.state, 2);

  const byLabel = new Map(second.elements.map((element) => [element.label, element.id]));
  assert.equal(byLabel.get("Alpha"), 1);
  assert.equal(byLabel.get("Beta"), 2);
  assert.equal(byLabel.get("Gamma"), 3);
});

test("a re-rendered node with a unique fingerprint keeps its id", () => {
  const doc = makeDoc('<button aria-label="Save">Save</button>');
  const button = doc.querySelector("button")!;
  stubRect(button, { x: 10, y: 10, w: 80, h: 30 });

  const first = run(doc, emptyIdentity(), 1);
  assert.equal(first.elements[0].id, 1);
  const fingerprint = elementFingerprint(button, first.elements[0]);

  const replacement = doc.createElement("button");
  replacement.setAttribute("aria-label", "Save");
  replacement.textContent = "Save";
  stubRect(replacement, { x: 10, y: 10, w: 80, h: 30 });
  button.replaceWith(replacement);

  const second = run(doc, first.state, 2);
  assert.equal(second.elements[0].id, 1);
  assert.equal(second.elements[0].change, "same");
  assert.equal(elementFingerprint(replacement, second.elements[0]), fingerprint);
  assert.deepEqual(second.removed, []);
});

test("disconnected elements are removed, culled connected ones keep identity", () => {
  const doc = makeDoc(`
    <button id="keep">Keep</button>
    <button id="drop">Drop</button>
  `);
  stubRect(doc.getElementById("keep")!, { x: 10, y: 10, w: 80, h: 30 });
  stubRect(doc.getElementById("drop")!, { x: 10, y: 50, w: 80, h: 30 });

  const first = run(doc, emptyIdentity(), 1);
  assert.deepEqual(ids(first), [1, 2]);

  doc.getElementById("drop")!.remove();
  const second = run(doc, first.state, 2);
  assert.deepEqual(ids(second), [1]);
  assert.deepEqual(second.removed, [2]);
});

test("a culled element keeps its id when it reappears", () => {
  const doc = makeDoc(`
    <button id="keep">Keep</button>
    <button id="far">Far</button>
  `);
  stubRect(doc.getElementById("keep")!, { x: 10, y: 10, w: 80, h: 30 });
  stubRect(doc.getElementById("far")!, { x: 10, y: 5000, w: 80, h: 30 });

  const first = run(doc, emptyIdentity(), 1);
  assert.deepEqual(ids(first), [1, 2]);

  // Viewport-only snapshot culls the far element; identity must survive.
  const culled = snapshot(doc, {}, VIEW);
  const culledApplied = applyIdentity(culled.elements, culled.byId, first.state, 2);
  assert.deepEqual(ids(culledApplied), [1]);
  assert.deepEqual(culledApplied.removed, []);

  const back = run(doc, culledApplied.state, 3);
  assert.deepEqual(ids(back), [1, 2]);
  assert.equal(back.elements[1].change, "same");
});
