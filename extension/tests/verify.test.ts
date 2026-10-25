import test from "node:test";
import assert from "node:assert/strict";

import { classifyReadBack, READBACK_MAX_CHARS } from "../src/background/verify.ts";

test("classifyReadBack accepts an identical value", () => {
  assert.equal(classifyReadBack("hello", "hello"), "exact");
});

test("classifyReadBack accepts whitespace-only differences", () => {
  assert.equal(classifyReadBack("hello world", "hello   world"), "normalized");
  assert.equal(classifyReadBack("line one\nline two", "line one line two"), "normalized");
  assert.equal(classifyReadBack(" padded ", "padded"), "normalized");
});

test("classifyReadBack reports an empty field separately from a mismatch", () => {
  assert.equal(classifyReadBack("hello", ""), "empty");
  assert.equal(classifyReadBack("hello", "goodbye"), "mismatch");
});

test("classifyReadBack compares only the first 1000 characters", () => {
  const prefix = "a".repeat(READBACK_MAX_CHARS);
  assert.equal(classifyReadBack(prefix + "tail", prefix), "exact");
  assert.equal(classifyReadBack(prefix + "tail", "b".repeat(READBACK_MAX_CHARS)), "mismatch");
});

test("classifyReadBack treats a rejected controlled input as empty, not a success", () => {
  const verdict = classifyReadBack("typed value", "");
  assert.equal(verdict, "empty");
  assert.notEqual(verdict, "exact");
  assert.notEqual(verdict, "normalized");
});
