import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import { extractFormState } from "../src/content/forms.ts";
import { buildAttrPayload, checkConsequence, resolveTargetByQuery } from "../src/content/interact.ts";
import { buildDescriptor, resolveFramePath, resolveTarget } from "../src/content/describe.ts";

function makeDoc(html: string): Document {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${html}</body></html>`);
  return dom.window.document;
}

function stubRect(el: Element, rect = { x: 10, y: 10, w: 100, h: 30 }): void {
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
  Object.defineProperty(el, "getClientRects", {
    configurable: true,
    value: () => [el.getBoundingClientRect()],
  });
}

test("extractFormState traverses top doc and nested iframes with correct state", () => {
  const doc = makeDoc(`
    <form id="top-form">
      <input type="text" name="username" id="user" value="alice" />
      <input type="checkbox" name="agree" id="terms" checked />
      <iframe id="quiz-frame" src="about:blank"></iframe>
    </form>
  `);

  const iframe = doc.getElementById("quiz-frame") as HTMLIFrameElement;
  const subDoc = iframe.contentDocument!;
  subDoc.body.innerHTML = `
    <div role="radiogroup">
      <label><input type="radio" name="q1" value="A" /> GPU</label>
      <label><input type="radio" name="q1" value="B" checked /> CPU</label>
    </div>
    <select name="tier"><option value="pro" selected>Pro</option></select>
    <button type="submit" disabled>Submit Quiz</button>
  `;

  const controls = extractFormState(doc);
  assert.equal(controls.length, 6);

  assert.equal(controls[0].frame, "top");
  assert.equal(controls[0].name, "username");
  assert.equal(controls[0].value, "alice");

  assert.equal(controls[1].frame, "top");
  assert.equal(controls[1].name, "agree");
  assert.equal(controls[1].checked, true);

  assert.equal(controls[2].frame, "f1");
  assert.equal(controls[2].name, "q1");
  assert.equal(controls[2].value, "A");
  assert.equal(controls[2].checked, false);

  assert.equal(controls[3].frame, "f1");
  assert.equal(controls[3].name, "q1");
  assert.equal(controls[3].value, "B");
  assert.equal(controls[3].checked, true);

  assert.equal(controls[4].frame, "f1");
  assert.equal(controls[4].tag, "select");
  assert.equal(controls[4].name, "tier");
  assert.equal(controls[4].value, "Pro");

  assert.equal(controls[5].frame, "f1");
  assert.equal(controls[5].tag, "button");
  assert.equal(controls[5].disabled, true);
});

test("buildAttrPayload supports scoping to frame by tag or selector", () => {
  const doc = makeDoc(`
    <iframe id="subframe" name="sub" src="about:blank"></iframe>
  `);

  const iframe = doc.getElementById("subframe") as HTMLIFrameElement;
  stubRect(iframe, { x: 50, y: 50, w: 500, h: 400 });
  const subDoc = iframe.contentDocument!;
  subDoc.body.innerHTML = `<input type="text" id="target-in-frame" value="hello frame" />`;
  const input = subDoc.getElementById("target-in-frame")!;
  stubRect(input, { x: 10, y: 15, w: 100, h: 25 });

  const attrByF1 = buildAttrPayload(doc, undefined, {
    selector: "#target-in-frame",
    frame: "f1",
  });
  assert.equal(attrByF1.value, "hello frame");
  assert.equal(attrByF1.elementId, "target-in-frame");

  const attrBySelector = buildAttrPayload(doc, undefined, {
    selector: "#target-in-frame",
    frame: "iframe#subframe",
  });
  assert.equal(attrBySelector.value, "hello frame");
});

test("buildDescriptor and resolveTarget roundtrip with framePath", async () => {
  const doc = makeDoc(`
    <iframe id="editor-frame" src="about:blank"></iframe>
  `);

  const iframe = doc.getElementById("editor-frame") as HTMLIFrameElement;
  stubRect(iframe, { x: 100, y: 100, w: 600, h: 400 });
  const subDoc = iframe.contentDocument!;
  subDoc.body.innerHTML = `<button id="framed-save" aria-label="Save Draft">Save Draft</button>`;
  const button = subDoc.getElementById("framed-save")!;
  stubRect(button, { x: 20, y: 30, w: 80, h: 30 });

  const desc = buildDescriptor(doc, button);
  assert.equal(desc.framePath, "iframe#editor-frame");
  assert.equal(desc.ariaLabel, "Save Draft");

  const index = new Map<number, Element>();
  const resolved = await resolveTarget(doc, index, desc, 0);
  assert.equal(resolved.label, "Save Draft");
  assert.equal(index.get(resolved.id), button);
});

test("resolveTargetByQuery resolves by text and css selector", () => {
  const doc = makeDoc(`
    <button id="btn-next">Next Step</button>
    <a href="/login" aria-label="Sign In">Sign In</a>
  `);
  const btn = doc.getElementById("btn-next")!;
  stubRect(btn, { x: 100, y: 200, w: 80, h: 30 });

  const index = new Map<number, Element>();
  const targetByText = resolveTargetByQuery(doc, index, { text: "Next Step" });
  assert.equal(targetByText.tag, "button");
  assert.equal(targetByText.label, "Next Step");
  assert.equal(targetByText.x, 100);

  const targetByCss = resolveTargetByQuery(doc, index, { selector: "#btn-next" });
  assert.equal(targetByCss.tag, "button");
});

test("checkConsequence identifies selection, dialogs, and navigation", () => {
  const doc = makeDoc(`
    <input type="radio" id="r1" name="q" value="cpu" checked />
    <dialog open aria-label="Confirmation Modal"></dialog>
  `);
  const r1 = doc.getElementById("r1")!;

  const index = new Map<number, Element>();
  index.set(1, r1);

  const radioConsequence = checkConsequence(doc, index, { id: 1, beforeChecked: false });
  assert.equal(radioConsequence, "selected (•)");

  const dialogConsequence = checkConsequence(doc, undefined, {});
  assert.equal(dialogConsequence, 'opened dialog "Confirmation Modal"');
});
