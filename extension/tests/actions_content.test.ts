import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import {
  buildMetricsPayload,
  buildStateEntries,
  buildTargetPayload,
  cssPathFor,
  focusIndexedElement,
  isEditableElement,
  waitForContent,
} from "../src/content/interact.ts";

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

function indexed(...elements: Element[]): Map<number, Element> {
  const map = new Map<number, Element>();
  elements.forEach((element, index) => map.set(index + 1, element));
  return map;
}

test("target returns fresh geometry, label, type, editability, and cssPath", () => {
  const doc = makeDoc('<div><input id="email" type="email" aria-label="Email address"></div>');
  const input = doc.querySelector("input")!;
  stubRect(input, { x: 10, y: 20, w: 200, h: 30 });

  const payload = buildTargetPayload(doc, new Map([[3, input]]), 3);

  assert.deepEqual(payload, {
    id: 3,
    tag: "input",
    type: "email",
    label: "Email address",
    x: 10,
    y: 20,
    w: 200,
    h: 30,
    interactable: true,
    reason: "",
    editable: true,
    cssPath: "body > div:nth-of-type(1) > input:nth-of-type(1)",
  });
});

test("target reports missing and detached elements with the snap hint", () => {
  const doc = makeDoc('<button aria-label="Go">Go</button>');
  const button = doc.querySelector("button")!;
  stubRect(button, { x: 1, y: 2, w: 30, h: 20 });

  assert.throws(
    () => buildTargetPayload(doc, indexed(button), 9),
    /element \[9\] not found; run snap first/,
  );

  button.remove();
  assert.throws(
    () => buildTargetPayload(doc, indexed(button), 1),
    /element \[1\] not found; run snap first/,
  );
});

test("target flags hidden, zero-size, pointer-events, and obscured elements", () => {
  const hiddenDoc = makeDoc('<button style="display:none">A</button>');
  const hidden = hiddenDoc.querySelector("button")!;
  stubRect(hidden, { x: 10, y: 10, w: 100, h: 30 });
  assert.deepEqual(
    (() => {
      const payload = buildTargetPayload(hiddenDoc, indexed(hidden), 1);
      return [payload.interactable, payload.reason];
    })(),
    [false, "hidden"],
  );

  const invisibleDoc = makeDoc('<button style="visibility:hidden">B</button>');
  const invisible = invisibleDoc.querySelector("button")!;
  stubRect(invisible, { x: 10, y: 10, w: 100, h: 30 });
  assert.equal(buildTargetPayload(invisibleDoc, indexed(invisible), 1).reason, "hidden");

  const tinyDoc = makeDoc('<button aria-label="Tiny">C</button>');
  const tiny = tinyDoc.querySelector("button")!;
  stubRect(tiny, { x: 10, y: 10, w: 0.4, h: 30 });
  assert.deepEqual(
    (() => {
      const payload = buildTargetPayload(tinyDoc, indexed(tiny), 1);
      return [payload.interactable, payload.reason];
    })(),
    [false, "zero size"],
  );

  const pointerDoc = makeDoc('<button style="pointer-events:none">D</button>');
  const pointer = pointerDoc.querySelector("button")!;
  stubRect(pointer, { x: 10, y: 10, w: 100, h: 30 });
  assert.deepEqual(
    (() => {
      const payload = buildTargetPayload(pointerDoc, indexed(pointer), 1);
      return [payload.interactable, payload.reason];
    })(),
    [false, "pointer-events: none"],
  );

  const obscuredDoc = makeDoc(
    '<button aria-label="Covered">E</button><div id="overlay">overlay</div>',
  );
  const covered = obscuredDoc.querySelector("button")!;
  const overlay = obscuredDoc.getElementById("overlay")!;
  stubRect(covered, { x: 10, y: 10, w: 100, h: 30 });
  Object.defineProperty(obscuredDoc, "elementFromPoint", {
    configurable: true,
    value: () => overlay,
  });
  const obscured = buildTargetPayload(obscuredDoc, indexed(covered), 1);
  assert.equal(obscured.interactable, false);
  assert.equal(obscured.reason, "obscured");
});

test("target accepts a hit inside the element as unobscured", () => {
  const doc = makeDoc('<button aria-label="Parent"><span id="inner">x</span></button>');
  const button = doc.querySelector("button")!;
  const inner = doc.getElementById("inner")!;
  stubRect(button, { x: 10, y: 10, w: 100, h: 30 });
  Object.defineProperty(doc, "elementFromPoint", {
    configurable: true,
    value: () => inner,
  });

  const payload = buildTargetPayload(doc, indexed(button), 1);
  assert.equal(payload.interactable, true);
  assert.equal(payload.reason, "");
});

test("editable covers textareas, text-like inputs, and contenteditable only", () => {
  const doc = makeDoc(`
    <textarea id="textarea"></textarea>
    <input id="text">
    <input id="search" type="search">
    <input id="checkbox" type="checkbox">
    <input id="hidden" type="hidden">
    <input id="file" type="file">
    <input id="range" type="range">
    <input id="color" type="color">
    <div id="editor" contenteditable="true"></div>
    <button id="button"></button>
  `);

  const ids = ["textarea", "text", "search", "checkbox", "hidden", "file", "range", "color", "editor", "button"];
  const expected = [true, true, true, false, false, false, false, false, true, false];

  ids.forEach((id, index) => {
    assert.equal(isEditableElement(doc.getElementById(id)!), expected[index], id);
  });
});

test("cssPath uses nth-of-type from body", () => {
  const doc = makeDoc(`
    <div><input id="first"></div>
    <div>
      <input id="a">
      <input id="b">
      <input id="third">
    </div>
  `);

  assert.equal(
    cssPathFor(doc.getElementById("first")!),
    "body > div:nth-of-type(1) > input:nth-of-type(1)",
  );
  assert.equal(
    cssPathFor(doc.getElementById("third")!),
    "body > div:nth-of-type(2) > input:nth-of-type(3)",
  );
  assert.equal(cssPathFor(doc.body), "body");
});

test("state lists submit-like controls with disabled from property or aria-disabled", () => {
  const doc = makeDoc(`
    <button id="save" aria-label="Save">Save</button>
    <input id="send" type="submit" aria-label="Send" aria-disabled="true">
    <input id="name" type="text" aria-label="Name">
    <input id="cancel" type="button" aria-label="Cancel" disabled>
    <input id="image" type="image" alt="Go">
    <input id="reset" type="reset" aria-label="Reset">
    <a href="/x">Link</a>
  `);

  const map = new Map<number, Element>();
  map.set(1, doc.getElementById("save")!);
  map.set(2, doc.getElementById("send")!);
  map.set(3, doc.getElementById("name")!);
  map.set(4, doc.getElementById("cancel")!);
  map.set(5, doc.getElementById("image")!);
  map.set(6, doc.getElementById("reset")!);
  map.set(7, doc.querySelector("a")!);

  const entries = buildStateEntries(doc, map);
  assert.deepEqual(entries, [
    { id: 1, tag: "button", type: "", label: "Save", disabled: false },
    { id: 2, tag: "input", type: "submit", label: "Send", disabled: true },
    { id: 4, tag: "input", type: "button", label: "Cancel", disabled: true },
    { id: 5, tag: "input", type: "image", label: "Go", disabled: false },
    { id: 6, tag: "input", type: "reset", label: "Reset", disabled: false },
  ]);
});

test("metrics reuse viewport math and add width", () => {
  const doc = makeDoc("<p>hi</p>");
  Object.defineProperty(doc.documentElement, "scrollHeight", { value: 3000 });

  const metrics = buildMetricsPayload(doc, {
    innerHeight: 800,
    innerWidth: 1200,
    scrollX: 0,
    scrollY: 200,
  });

  assert.deepEqual(metrics, {
    scrollY: 200,
    viewportHeight: 800,
    viewportWidth: 1200,
    totalHeight: 3000,
    percent: 33,
  });

  const fallback = buildMetricsPayload(doc);
  assert.equal(fallback.viewportWidth, 1024);
});

test("focus places the caret at the end of an input", () => {
  const doc = makeDoc('<input id="name" type="text" value="hello">');
  const input = doc.getElementById("name") as HTMLInputElement;
  stubRect(input, { x: 10, y: 10, w: 100, h: 30 });

  focusIndexedElement(doc, indexed(input), 1);

  assert.equal(doc.activeElement, input);
  assert.equal(input.selectionStart, 5);
  assert.equal(input.selectionEnd, 5);
});

test("focus collapses a selection at the end of contenteditable", () => {
  const doc = makeDoc('<div id="editor" contenteditable="true">hello</div>');
  const editor = doc.getElementById("editor")!;
  stubRect(editor, { x: 10, y: 10, w: 100, h: 30 });

  focusIndexedElement(doc, indexed(editor), 1);

  assert.equal(doc.activeElement, editor);
  const selection = doc.defaultView!.getSelection();
  assert.ok(selection);
  assert.equal(selection!.isCollapsed, true);
  assert.equal(selection!.anchorNode, editor);
});

test("focus rejects missing ids", () => {
  const doc = makeDoc("<p>hi</p>");
  assert.throws(() => focusIndexedElement(doc, new Map(), 4), /element \[4\] not found/);
});

test("selector wait resolves immediately when the element already exists", async () => {
  const doc = makeDoc('<div id="present">here</div>');
  const result = await waitForContent(doc, { selector: "#present", timeoutMs: 500 });
  assert.deepEqual(result, { waitedMs: result.waitedMs, matched: true });
  assert.ok(result.waitedMs < 100);
});

test("selector wait resolves on a later DOM mutation", async () => {
  const doc = makeDoc("<div id='root'></div>");
  const pending = waitForContent(doc, { selector: "#late", timeoutMs: 1000 });

  setTimeout(() => {
    const button = doc.createElement("button");
    button.id = "late";
    doc.body.appendChild(button);
  }, 20);

  const result = await pending;
  assert.equal(result.matched, true);
  assert.ok(result.waitedMs >= 10);
});

test("selector wait returns matched:false on timeout", async () => {
  const doc = makeDoc("<div id='root'></div>");
  const result = await waitForContent(doc, { selector: "#never", timeoutMs: 40 });
  assert.equal(result.matched, false);
  assert.ok(result.waitedMs >= 30);
});

test("invalid selectors reject", async () => {
  const doc = makeDoc("<div></div>");
  await assert.rejects(
    waitForContent(doc, { selector: "###", timeoutMs: 100 }),
    /invalid selector/,
  );
});

test("idle wait resolves after a quiet period once the document is complete", async () => {
  const doc = makeDoc("<div>ready</div>");
  Object.defineProperty(doc, "readyState", { configurable: true, value: "complete" });

  const result = await waitForContent(doc, { idle: true, timeoutMs: 2000 });
  assert.equal(result.matched, true);
  assert.ok(result.waitedMs >= 550, `waitedMs=${result.waitedMs}`);
});

test("idle wait can time out", async () => {
  const doc = makeDoc("<div>loading</div>");
  Object.defineProperty(doc, "readyState", { configurable: true, value: "loading" });

  const result = await waitForContent(doc, { idle: true, timeoutMs: 80 });
  assert.equal(result.matched, false);
  assert.ok(result.waitedMs >= 70);
});
