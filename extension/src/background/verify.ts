/** Pure read-back comparison for `action.type`; no chrome.* access. */

export const READBACK_MAX_CHARS = 1_000;

export type Verification = "exact" | "normalized" | "empty" | "mismatch";

export function normalizeText(value: string): string {
  return value.replace(/\s+/g, " ").trim();
}

export function classifyReadBack(expected: string, actual: string): Verification {
  const target =
    expected.length > READBACK_MAX_CHARS ? expected.slice(0, READBACK_MAX_CHARS) : expected;
  if (actual === target) {
    return "exact";
  }
  if (normalizeText(actual) === normalizeText(target)) {
    return "normalized";
  }
  return actual === "" ? "empty" : "mismatch";
}
