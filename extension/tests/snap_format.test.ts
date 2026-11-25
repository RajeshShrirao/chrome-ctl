import test from "node:test";
import assert from "node:assert/strict";

import {
  SNAP_HEADER_QUERY_HASH_MAX_CHARS,
  formatSnapDiff,
  formatSnapHeaderUrl,
} from "../src/background/snap_format.ts";
import type { SnapElement } from "../src/shared/protocol.js";

function makeElement(overrides: Partial<SnapElement> = {}): SnapElement {
  return {
    id: 1,
    tag: "button",
    type: "",
    label: "Save",
    value: "",
    state: "",
    x: 10,
    y: 20,
    w: 80,
    h: 30,
    ...overrides,
  };
}

test("formatSnapHeaderUrl keeps short URLs and path-only URLs verbatim", () => {
  assert.equal(formatSnapHeaderUrl(""), "");

  const longPathOnly = `https://example.test/${"segment/".repeat(20)}page`;
  assert.equal(formatSnapHeaderUrl(longPathOnly), longPathOnly);

  const exact80Suffix = `?${"a".repeat(SNAP_HEADER_QUERY_HASH_MAX_CHARS - 1)}`;
  const exactUrl = `https://example.test/search${exact80Suffix}`;
  assert.equal(formatSnapHeaderUrl(exactUrl), exactUrl);

  const shortQueryAndHash = "https://example.test/search?q=hello#top";
  assert.equal(formatSnapHeaderUrl(shortQueryAndHash), shortQueryAndHash);
});

test("formatSnapHeaderUrl truncates query+hash exceeding 80 chars cleanly", () => {
  const query = `?q=${"x".repeat(97)}`; // length 100
  const url = `https://example.test/search${query}`;
  const formatted = formatSnapHeaderUrl(url);

  assert.equal(
    formatted,
    `https://example.test/search${query.slice(0, SNAP_HEADER_QUERY_HASH_MAX_CHARS)}\u2026 (+20 chars)`,
  );

  const hashOnly = `#${"h".repeat(85)}`; // length 86
  assert.equal(
    formatSnapHeaderUrl(`https://example.test/docs${hashOnly}`),
    `https://example.test/docs${hashOnly.slice(0, SNAP_HEADER_QUERY_HASH_MAX_CHARS)}\u2026 (+6 chars)`,
  );

  const queryPlusHash = `?a=1&b=2#${"f".repeat(82)}`; // length 91
  assert.equal(
    formatSnapHeaderUrl(`https://example.test/app${queryPlusHash}`),
    `https://example.test/app${queryPlusHash.slice(0, SNAP_HEADER_QUERY_HASH_MAX_CHARS)}\u2026 (+11 chars)`,
  );
});

test("formatSnapDiff suppresses pure coordinate shifts into moved count", () => {
  const headerLines = ["SNAP 2 | TAB [1] Page | https://example.test/"];
  const prev = [
    makeElement({ id: 1, tag: "button", label: "First", x: 10, y: 100 }),
    makeElement({ id: 2, tag: "a", label: "Second", x: 10, y: 200 }),
    makeElement({ id: 3, tag: "input", label: "Name", value: "", x: 10, y: 300 }),
  ];
  const next = [
    makeElement({ id: 1, tag: "button", label: "First", x: 10, y: 20, change: "moved" }),
    makeElement({ id: 2, tag: "a", label: "Second", x: 15, y: 120 }),
    makeElement({ id: 3, tag: "input", label: "Name", value: "", x: 10, y: 300, change: "same" }),
  ];

  const text = formatSnapDiff(headerLines, prev, next, []);
  assert.equal(
    text,
    [
      ...headerLines,
      "DIFF: no interactive elements added, changed, or removed, 2 moved.",
    ].join("\n"),
  );
  assert.ok(!text.includes("~ ["));
});

test("formatSnapDiff lists added, removed, and semantic changes while counting moved separately", () => {
  const headerLines = ["SNAP 3 | TAB [1] Page | https://example.test/"];
  const prev = [
    makeElement({ id: 1, tag: "button", label: "Save", state: "", x: 10, y: 100 }),
    makeElement({ id: 2, tag: "button", label: "Cancel", x: 10, y: 150 }),
    makeElement({ id: 3, tag: "input", label: "Email", value: "", x: 10, y: 200 }),
    makeElement({ id: 4, tag: "a", label: "Old Link", x: 10, y: 250 }),
  ];
  const next = [
    makeElement({ id: 1, tag: "button", label: "Save", state: "disabled", x: 10, y: 80, change: "updated" }),
    makeElement({ id: 2, tag: "button", label: "Cancel", x: 10, y: 120, change: "moved" }),
    makeElement({ id: 3, tag: "input", label: "Email", value: "a@b.c", x: 10, y: 200 }),
    makeElement({ id: 5, tag: "button", label: "New", x: 10, y: 300, change: "added" }),
  ];

  const text = formatSnapDiff(headerLines, prev, next, [4]);
  assert.equal(
    text,
    [
      ...headerLines,
      "DIFF (+1 added, ~2 changed, -1 removed, 1 moved):",
      '+ [5] <button> "New" @10,300',
      '~ [1] <button> "Save" @10,80',
      '~ [3] <input> "Email" @10,200',
      '- [4] <a> "Old Link"',
    ].join("\n"),
  );
});

test("formatSnapDiff detects tag/label/state differences without relying on el.change", () => {
  const headerLines = ["SNAP 4 | TAB [1] Page | https://example.test/"];
  const prev = [
    makeElement({ id: 1, tag: "button", label: "Go", state: "" }),
    makeElement({ id: 2, tag: "a", label: "Docs", state: "" }),
  ];
  const next = [
    makeElement({ id: 1, tag: "a", label: "Go", state: "" }),
    makeElement({ id: 2, tag: "a", label: "Docs", state: "expanded" }),
  ];

  const text = formatSnapDiff(headerLines, prev, next, []);
  assert.ok(text.includes("DIFF (+0 added, ~2 changed, -0 removed):"));
  assert.ok(text.includes('~ [1] <a> "Go" @10,20'));
  assert.ok(text.includes('~ [2] <a> "Docs" @10,20'));
});

test("formatSnapDiff omits moved count when zero elements moved", () => {
  const headerLines = ["SNAP 5 | TAB [1] Page | https://example.test/"];
  const prev = [makeElement({ id: 1 })];
  const next = [makeElement({ id: 1, change: "same" })];

  assert.equal(
    formatSnapDiff(headerLines, prev, next, []),
    [...headerLines, "DIFF: no interactive elements added, changed, or removed."].join("\n"),
  );
});
