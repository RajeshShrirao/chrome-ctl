import test from "node:test";
import assert from "node:assert/strict";

import { describeCaptureError, stripPngDataUrl } from "../src/background/capture.ts";

const PNG_PREFIX = "data:image/png;base64,";

test("stripPngDataUrl removes the PNG data URL prefix", () => {
  assert.equal(stripPngDataUrl(`${PNG_PREFIX}iVBORw0KGgo=`), "iVBORw0KGgo=");
  assert.equal(stripPngDataUrl(`${PNG_PREFIX}AAAA+/==`), "AAAA+/==");
});

test("stripPngDataUrl tolerates whitespace and prefix casing", () => {
  assert.equal(stripPngDataUrl(`  ${PNG_PREFIX}abc  `), "abc");
  assert.equal(stripPngDataUrl("DATA:IMAGE/PNG;BASE64,abc"), "abc");
});

test("stripPngDataUrl rejects non-PNG data URLs", () => {
  assert.throws(
    () => stripPngDataUrl("data:image/jpeg;base64,abc"),
    /did not return a PNG data URL/,
  );
  assert.throws(
    () => stripPngDataUrl("data:image/webp;base64,abc"),
    /did not return a PNG data URL/,
  );
});

test("stripPngDataUrl rejects values that are not data URLs", () => {
  assert.throws(() => stripPngDataUrl("iVBORw0KGgo="), /did not return a data URL/);
  assert.throws(
    () => stripPngDataUrl("https://example.test/shot.png"),
    /did not return a data URL/,
  );
  assert.throws(() => stripPngDataUrl(""), /did not return a data URL/);
});

test("stripPngDataUrl rejects an empty PNG payload", () => {
  assert.throws(() => stripPngDataUrl(PNG_PREFIX), /empty PNG payload/);
  assert.throws(() => stripPngDataUrl(`  ${PNG_PREFIX}  `), /empty PNG payload/);
});

test("describeCaptureError maps rate limiting to a concise message", () => {
  const message = describeCaptureError(
    new Error("MAX_CAPTURE_VISIBLE_TAB_CALLS_PER_SECOND quota exceeded"),
  );
  assert.match(message, /capture rate limit exceeded/);
  assert.match(message, /MAX_CAPTURE_VISIBLE_TAB_CALLS_PER_SECOND/);
  assert.match(message, /retry shortly/);
});

test("describeCaptureError maps permission and restricted pages", () => {
  for (const raw of [
    "Cannot access contents of the page. Extension manifest must request permission.",
    "The extensions gallery cannot be scripted.",
    "Cannot capture a chrome:// page.",
    "The activeTab permission is not in effect in the current tab.",
  ]) {
    const message = describeCaptureError(new Error(raw));
    assert.ok(
      message.startsWith("cannot capture this page: "),
      `unexpected message for ${raw}: ${message}`,
    );
  }
});

test("describeCaptureError falls back to a generic capture failure", () => {
  assert.equal(
    describeCaptureError(new Error("display went away")),
    "capture failed: display went away",
  );
  assert.equal(
    describeCaptureError("display went away"),
    "capture failed: display went away",
  );
  assert.equal(describeCaptureError(undefined), "capture failed: unknown error");
  assert.equal(describeCaptureError({ message: "ignored" }), "capture failed: unknown error");
});
