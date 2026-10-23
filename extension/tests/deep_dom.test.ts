import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import {
  addFrameOffset,
  findVisibleModal,
  snapshot,
  viewportRect,
} from "../src/content/dom.ts";
import type { WindowLike } from "../src/content/dom.ts";
import { queryDeepAll, resolveTarget } from "../src/content/describe.ts";

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

function stubClientRects(el: Element): void {
  Object.defineProperty(el, "getClientRects", {
    configurable: true,
    value: () => [el.getBoundingClientRect()],
  });
}

function labels(snap: ReturnType<typeof snapshot>): string[] {
  return snap.elements.map((element) => element.label);
}

test("snapshot includes open and nested shadow roots in composed order", () => {
  const doc = makeDoc('<div id="light"></div><div id="host"></div>');
  const lightButton = doc.createElement("button");
  lightButton.id = "light-button";
  lightButton.textContent = "Light";
  doc.getElementById("light")!.appendChild(lightButton);

  const root = doc.getElementById("host")!.attachShadow({ mode: "open" });
  root.innerHTML = `
    <label for="shadow-input">Shadow name label</label>
    <input id="shadow-input" aria-label="Shadow name" />
    <button id="shadow-button" data-testid="shadow-button">Save shadow</button>
    <div id="nested-host"></div>
  `;

  const nestedHost = root.querySelector("#nested-host")!;
  const nestedRoot = nestedHost.attachShadow({ mode: "open" });
  nestedRoot.innerHTML =
    '<button id="deep-button" aria-label="Deep action">Deep action</button>';

  const shadowInput = root.querySelector("#shadow-input")!;
  const shadowButton = root.querySelector("#shadow-button")!;
  const deepButton = nestedRoot.querySelector("#deep-button")!;

  [lightButton, shadowInput, shadowButton, deepButton].forEach((el, index) => {
    stubRect(el, { x: 10, y: 10 + index * 50, w: 140, h: 30 });
  });

  const snap = snapshot(doc, { all: true }, VIEW);

  assert.deepEqual(
    snap.elements.map((element) => [element.tag, element.label]),
    [
      ["button", "Light"],
      ["input", "Shadow name"],
      ["button", "Save shadow"],
      ["button", "Deep action"],
    ],
  );
  assert.deepEqual(
    snap.elements.map((element) => element.id),
    [1, 2, 3, 4],
  );
  assert.equal(snap.byId.get(2), shadowInput);
  assert.equal(snap.byId.get(3), shadowButton);
  assert.equal(snap.byId.get(4), deepButton);
  assert.equal(snap.elements[1].type, "text");
  assert.equal(snap.elements[1].x, 10);
  assert.equal(snap.elements[1].y, 60);
});

test("snapshot culls elements covered by a modal, keeps modal content, and skips hit-testing without a modal", () => {
  const modalDoc = makeDoc(`
    <button id="under" aria-label="Underlying">Underlying</button>
    <dialog id="modal" open aria-modal="true" style="display:block">
      <button id="confirm" aria-label="Confirm">Confirm</button>
    </dialog>
  `);
  const under = modalDoc.getElementById("under")!;
  const modal = modalDoc.getElementById("modal")!;
  const confirm = modalDoc.getElementById("confirm")!;
  stubRect(under, { x: 10, y: 10, w: 200, h: 40 });
  stubRect(modal, { x: 100, y: 100, w: 400, h: 200 });
  stubRect(confirm, { x: 150, y: 150, w: 120, h: 40 });

  const modalElement = findVisibleModal(modalDoc);
  assert.equal(modalElement, modal);

  let hitCalls = 0;
  const modalSnap = snapshot(
    modalDoc,
    {
      all: true,
      hitTest: () => {
        hitCalls += 1;
        return modal;
      },
    },
    VIEW,
  );

  assert.deepEqual(labels(modalSnap), ["Confirm"]);
  // Only the outside element is hit-tested; composed containment exempts Confirm.
  assert.equal(hitCalls, 1);
  assert.equal(modalSnap.byId.get(1), confirm);

  const plainDoc = makeDoc('<button id="a">A</button><button id="b">B</button>');
  stubRect(plainDoc.getElementById("a")!, { x: 10, y: 10, w: 100, h: 30 });
  stubRect(plainDoc.getElementById("b")!, { x: 10, y: 60, w: 100, h: 30 });

  assert.equal(findVisibleModal(plainDoc), null);
  let plainHitCalls = 0;
  const plainSnap = snapshot(
    plainDoc,
    {
      all: true,
      hitTest: () => {
        plainHitCalls += 1;
        return null;
      },
    },
    VIEW,
  );
  assert.deepEqual(labels(plainSnap), ["A", "B"]);
  assert.equal(plainHitCalls, 0);
});

test("addFrameOffset composes nested frame offsets in top-frame pixels", () => {
  const inner = { left: 5, top: 7, width: 10, height: 10 };
  const afterInner = addFrameOffset(
    inner,
    { left: 100, top: 200 },
    2,
    3,
  );
  assert.deepEqual(afterInner, { left: 107, top: 210, width: 10, height: 10 });

  const afterOuter = addFrameOffset(
    afterInner,
    { left: 1000, top: 2000 },
    5,
    6,
  );
  assert.deepEqual(afterOuter, { left: 1112, top: 2216, width: 10, height: 10 });
});

test("viewportRect matches the element rect when it is not framed", () => {
  const doc = makeDoc('<button id="plain">Plain</button>');
  const button = doc.getElementById("plain")!;
  stubRect(button, { x: 12, y: 34, w: 56, h: 78 });

  assert.deepEqual(viewportRect(button), {
    left: 12,
    top: 34,
    width: 56,
    height: 78,
  });
});

test("snapshot traverses same-origin iframes and reports top-frame coordinates", () => {
  const doc = makeDoc('<button id="top" aria-label="Top">Top</button><iframe id="frame"></iframe>');
  const topButton = doc.getElementById("top")!;
  const frame = doc.getElementById("frame") as HTMLIFrameElement;
  frame.contentDocument!.body.innerHTML =
    '<button id="framed" aria-label="Framed" style="width:50px;height:20px">Framed</button>';
  const framed = frame.contentDocument!.getElementById("framed")!;

  stubRect(topButton, { x: 10, y: 10, w: 100, h: 30 });
  stubRect(frame, { x: 100, y: 200, w: 400, h: 300 });
  stubRect(framed, { x: 5, y: 7, w: 50, h: 20 });
  Object.defineProperty(frame, "clientLeft", { configurable: true, value: 2 });
  Object.defineProperty(frame, "clientTop", { configurable: true, value: 3 });

  const snap = snapshot(doc, { all: true }, VIEW);
  assert.deepEqual(labels(snap), ["Top", "Framed"]);
  const framedElement = snap.elements[1];
  assert.equal(framedElement.x, 107);
  assert.equal(framedElement.y, 210);
  assert.equal(snap.byId.get(2), framed);

  // Without `all`, content scrolled outside the iframe's content box is culled.
  stubRect(framed, { x: 500, y: 400, w: 50, h: 20 });
  const clipped = snapshot(doc, {}, VIEW);
  assert.deepEqual(labels(clipped), ["Top"]);
});

test("resolver tier search finds an element inside an open shadow root", async () => {
  const doc = makeDoc('<div id="host"></div>');
  const root = doc.getElementById("host")!.attachShadow({ mode: "open" });
  root.innerHTML =
    '<button data-testid="shadow-action" aria-label="Shadow action">Shadow action</button>';
  const button = root.querySelector("button")!;

  stubRect(button, { x: 40, y: 60, w: 120, h: 32 });
  stubClientRects(button);

  assert.deepEqual(queryDeepAll(doc, '[data-testid="shadow-action"]'), [button]);

  const index = new Map<number, Element>();
  const resolved = await resolveTarget(
    doc,
    index,
    { testId: "shadow-action" },
    0,
  );

  assert.equal(resolved.matchedTier, "testId");
  assert.equal(resolved.tag, "button");
  assert.equal(resolved.type, "");
  assert.equal(resolved.label, "Shadow action");
  assert.deepEqual(
    { x: resolved.x, y: resolved.y, w: resolved.w, h: resolved.h },
    { x: 40, y: 60, w: 120, h: 32 },
  );
  assert.equal(index.get(resolved.id), button);
});
