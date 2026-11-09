import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

type Listener = (
  message: unknown,
  sender: unknown,
  sendResponse: (response: unknown) => void,
) => boolean | void;

const listeners: Listener[] = [];
const dom = new JSDOM(
  `<!doctype html><html><body>
    <button id="a">Alpha</button>
    <a id="b" href="/next">Beta</a>
  </body></html>`,
  { url: "https://example.com/" },
);

const domWindow = dom.window;
(globalThis as unknown as { window: Window }).window = domWindow as unknown as Window;
(globalThis as unknown as { document: Document }).document = domWindow.document;
(globalThis as unknown as { chrome: unknown }).chrome = {
  runtime: { onMessage: { addListener: (fn: Listener) => listeners.push(fn) } },
};

await import("../src/content/indexer.ts");

let y = 10;
function layout(selector: string): void {
  for (const el of Array.from(domWindow.document.querySelectorAll(selector))) {
    const top = y;
    y += 40;
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

layout("button, a");

function request(payload: Record<string, unknown>): Promise<any> {
  return new Promise((resolve) => {
    listeners[0]({ channel: "chromeCtl", ...payload }, {}, resolve);
  });
}

async function snap(extra: Record<string, unknown> = {}): Promise<any> {
  const response = await request({ type: "snap", all: false, formsOnly: false, ...extra });
  assert.equal(response.ok, true, response.error);
  return response.snap;
}

test("snapshot ids stay stable across polls and stale identity is rejected", async () => {
  const first = await snap();
  assert.equal(typeof first.docId, "string");
  assert.notEqual(first.docId, "");
  assert.equal(first.snapshotId, 1);
  assert.deepEqual(
    first.elements.map((el: { id: number }) => el.id),
    [1, 2],
  );
  assert.deepEqual(
    first.elements.map((el: { change?: string }) => el.change),
    ["added", "added"],
  );
  assert.deepEqual(first.removed, []);

  const second = await snap();
  assert.equal(second.docId, first.docId);
  assert.equal(second.snapshotId, 2);
  // Same DOM nodes keep their ids and report no change.
  assert.deepEqual(
    second.elements.map((el: { id: number }) => el.id),
    [1, 2],
  );
  assert.deepEqual(
    second.elements.map((el: { change?: string }) => el.change),
    ["same", "same"],
  );
  assert.deepEqual(second.removed, []);

  const target = await request({ type: "target", id: 1 });
  assert.equal(target.ok, true, target.error);
  assert.equal(target.target.label, "Alpha");
  assert.equal(target.target.snapshotId, 2);

  // Re-rendering replaces the node. A unique fingerprint keeps the id, which
  // is what makes macro replay and diffing survive framework re-renders.
  const oldButton = domWindow.document.getElementById("a")!;
  const replacement = domWindow.document.createElement("button");
  replacement.id = "a";
  replacement.textContent = "Alpha";
  Object.defineProperty(replacement, "getBoundingClientRect", {
    configurable: true,
    value: () => ({
      x: 10,
      y: 10,
      left: 10,
      top: 10,
      right: 110,
      bottom: 40,
      width: 100,
      height: 30,
      toJSON: () => ({}),
    }),
  });
  oldButton.replaceWith(replacement);

  const third = await snap();
  assert.equal(third.snapshotId, 3);
  assert.deepEqual(
    third.elements.map((el: { id: number }) => el.id),
    [1, 2],
  );
  assert.deepEqual(
    third.elements.map((el: { change?: string }) => el.change),
    ["same", "same"],
  );

  const replaced = await request({ type: "target", id: 1 });
  assert.equal(replaced.ok, true, replaced.error);
  assert.equal(replaced.target.label, "Alpha");

  // Removing an element reports its id as removed and drops it from the index.
  domWindow.document.getElementById("b")!.remove();
  const fourth = await snap();
  assert.deepEqual(
    fourth.elements.map((el: { id: number }) => el.id),
    [1],
  );
  assert.deepEqual(fourth.removed, [2]);

  const gone = await request({ type: "target", id: 2 });
  assert.equal(gone.ok, false);
  assert.match(gone.error, /element \[2\] not found/);

  // Pre-navigation ids are rejected as soon as the document identity changes.
  const previousDocId = fourth.docId;
  delete (domWindow as unknown as { __chromeCtlDocId?: string }).__chromeCtlDocId;
  const fifth = await snap();
  assert.notEqual(fifth.docId, previousDocId);
  assert.equal(fifth.snapshotId, 5);
  assert.deepEqual(
    fifth.elements.map((el: { change?: string }) => el.change),
    ["added"],
  );
  const freshId = fifth.elements[0].id as number;

  const stale = await request({
    type: "target",
    id: freshId,
    expectedDocId: previousDocId,
  });
  assert.equal(stale.ok, false);
  assert.match(stale.error, /snapshot stale/);

  // The fresh identity is accepted.
  const fresh = await request({
    type: "target",
    id: freshId,
    expectedDocId: fifth.docId,
  });
  assert.equal(fresh.ok, true, fresh.error);
});
