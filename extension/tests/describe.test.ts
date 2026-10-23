import test from "node:test";
import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

import {
  accessibleName,
  buildDescriptor,
  cssEscape,
  cssSelectorFor,
  nextIndexId,
  normalizeText,
  roleFor,
  testIdFor,
  xpathFor,
} from "../src/content/describe.ts";

function makeDoc(html: string): Document {
  const dom = new JSDOM(`<!doctype html><html><head></head><body>${html}</body></html>`);
  return dom.window.document;
}

test("normalizeText collapses whitespace and trims", () => {
  assert.equal(normalizeText("  Hello \n\t world  "), "Hello world");
  assert.equal(normalizeText(""), "");
  assert.equal(normalizeText(null), "");
  assert.equal(normalizeText(undefined), "");
});

test("testId uses the first non-empty data attribute in stable order", () => {
  const doc = makeDoc(`
    <button data-test="legacy" data-qa="qa" data-cy="cy">A</button>
    <button data-qa="qa-only">B</button>
    <button data-testid="preferred" data-test="legacy">C</button>
  `);
  const buttons = doc.querySelectorAll("button");
  assert.equal(testIdFor(buttons[0]), "legacy");
  assert.equal(testIdFor(buttons[1]), "qa-only");
  assert.equal(testIdFor(buttons[2]), "preferred");
});

test("accessible name precedence: aria-label, labelledby, label, placeholder, text", () => {
  const doc = makeDoc(`
    <span id="lbl">From labelledby</span>
    <input id="i1" aria-label="Direct" aria-labelledby="lbl" placeholder="P">
    <input id="i2" aria-labelledby="lbl" placeholder="P">
    <label for="i3">Label text</label>
    <input id="i3" placeholder="P">
    <label>Wrapped <input id="i4"></label>
    <input id="i5" placeholder="Placeholder">
    <button id="i6">Visible <span>text</span></button>
  `);

  const byId = (id: string) => doc.getElementById(id)!;
  assert.equal(accessibleName(byId("i1"), doc), "Direct");
  assert.equal(accessibleName(byId("i2"), doc), "From labelledby");
  assert.equal(accessibleName(byId("i3"), doc), "Label text");
  assert.equal(accessibleName(byId("i4"), doc), "Wrapped");
  assert.equal(accessibleName(byId("i5"), doc), "Placeholder");
  assert.equal(accessibleName(byId("i6"), doc), "Visible text");
});

test("roles use explicit attributes, then implicit HTML mappings", () => {
  const doc = makeDoc(`
    <button>B</button>
    <a href="/x">L</a>
    <a>No href</a>
    <input type="checkbox">
    <input type="radio">
    <input>
    <textarea></textarea>
    <select></select>
    <div role="switch"></div>
    <div role="button" aria-label="x"></div>
  `);
  const elements = Array.from(doc.body.querySelectorAll("*"));
  assert.deepEqual(
    elements.map((el) => roleFor(el)),
    ["button", "link", "", "checkbox", "radio", "textbox", "textbox", "combobox", "switch", "button"],
  );
});

test("descriptor carries test id, accessible name, role, tag, type, and selectors", () => {
  const doc = makeDoc('<input type="checkbox" data-cy="agree" aria-label="I agree">');
  const target = buildDescriptor(doc, doc.querySelector("input")!);

  assert.equal(target.testId, "agree");
  assert.equal(target.ariaLabel, "I agree");
  assert.equal(target.role, "checkbox");
  assert.equal(target.tag, "input");
  assert.equal(target.type, "checkbox");
  assert.equal(target.text, undefined);
  assert.equal(target.xpath, "//body/input[1]");
  assert.equal(target.cssSelector, '[data-cy="agree"]');
});

test("descriptor only includes text at or below 80 characters", () => {
  const doc = makeDoc(`
    <button>  Save \n now </button>
    <button>${"alpha ".repeat(30)}</button>
  `);
  const [short, long] = Array.from(doc.querySelectorAll("button"));

  const shortTarget = buildDescriptor(doc, short);
  assert.equal(shortTarget.text, "Save now");
  assert.equal(shortTarget.ariaLabel, "Save now");

  const longTarget = buildDescriptor(doc, long);
  assert.equal(longTarget.text, undefined);
  assert.equal(longTarget.ariaLabel, normalizeText("alpha ".repeat(30)).slice(0, 80));
  assert.equal(longTarget.ariaLabel!.length, 80);
});

test("xpath prefers ids and otherwise builds a positional path from body", () => {
  const doc = makeDoc(`
    <button id="save">Save</button>
    <div><div><button>One</button><button>Two</button></div></div>
  `);
  const buttons = doc.querySelectorAll("button");
  assert.equal(xpathFor(buttons[0]), '//*[@id="save"]');
  assert.equal(xpathFor(buttons[2]), "//body/div[1]/div[1]/button[2]");
});

test("css selector prefers authored ids, then test ids, names, structural chains", () => {
  const doc = makeDoc(`
    <button id="save">Save</button>
    <div id="a1b2c3d4e5f6a7b8"><span data-testid="row-1">Row</span></div>
    <input id="f3c2b1a0f9e8d7c6" name="email">
    <div><section><div><span><em>Deep</em></span></div></section></div>
  `);

  assert.equal(cssSelectorFor(doc.getElementById("save")!), "#save");
  assert.equal(
    cssSelectorFor(doc.querySelector("[data-testid]")!),
    '[data-testid="row-1"]',
  );
  assert.equal(
    cssSelectorFor(doc.querySelector("input[name]")!),
    '[name="email"]',
  );
  assert.equal(
    cssSelectorFor(doc.querySelector("em")!),
    "div:nth-of-type(2) > section:nth-of-type(1) > div:nth-of-type(1) > span:nth-of-type(1) > em:nth-of-type(1)",
  );
});

test("cssEscape escapes selector-significant characters", () => {
  assert.equal(cssEscape("plain-id"), "plain-id");
  assert.equal(cssEscape("a b"), "a\\ b");
  assert.equal(cssEscape('a"b'), 'a\\"b');
  assert.equal(cssEscape("9lives"), "\\39 lives");
});

test("css selector escapes quoted attribute values", () => {
  const doc = makeDoc('<span data-testid=\'say "hi"\'>X</span>');
  assert.equal(
    cssSelectorFor(doc.querySelector("span")!),
    '[data-testid="say\\ \\"hi\\""]',
  );
});

test("nextIndexId allocates after the largest existing snapshot id", () => {
  const doc = makeDoc("<button>A</button>");
  const element = doc.querySelector("button")!;

  assert.equal(nextIndexId(new Map()), 1);
  assert.equal(nextIndexId(new Map([[1, element]])), 2);
  assert.equal(
    nextIndexId(
      new Map([
        [2, element],
        [5, element],
      ]),
    ),
    6,
  );
});
