/**
 * Pure helpers for media.capture: data-URL validation and error shaping.
 * Deliberately free of chrome.* references so node:test can cover them.
 */

const PNG_DATA_URL_PREFIX = "data:image/png;base64,";
const RATE_LIMIT_MARKER = "MAX_CAPTURE_VISIBLE_TAB_CALLS_PER_SECOND";

const RESTRICTED_MARKERS = [
  "cannot access contents of the page",
  "cannot capture",
  "cannot be scripted",
  "extensions gallery",
  "not allowed",
  "permission",
  "restricted",
  "showing a modal",
  "activetab",
  "chrome://",
  "chrome-extension://",
  "chrome-untrusted://",
  "devtools://",
];

/**
 * Strip the `data:image/png;base64,` prefix from a captureVisibleTab result.
 * Throws when the value is not a non-empty PNG data URL.
 */
export function stripPngDataUrl(dataUrl: string): string {
  if (typeof dataUrl !== "string") {
    throw new Error("capture did not return a data URL");
  }

  const trimmed = dataUrl.trim();
  if (!trimmed.toLowerCase().startsWith("data:")) {
    throw new Error("capture did not return a data URL");
  }
  if (!trimmed.toLowerCase().startsWith(PNG_DATA_URL_PREFIX)) {
    throw new Error("capture did not return a PNG data URL");
  }

  const base64 = trimmed.slice(PNG_DATA_URL_PREFIX.length);
  if (base64 === "") {
    throw new Error("capture returned an empty PNG payload");
  }
  return base64;
}

/** Turn a chrome.tabs.captureVisibleTab failure into a concise RPC message. */
export function describeCaptureError(error: unknown): string {
  let raw = "";
  if (error instanceof Error) {
    raw = error.message;
  } else if (typeof error === "string") {
    raw = error;
  }
  const text = raw.trim() === "" ? "unknown error" : raw.trim();

  if (text.includes(RATE_LIMIT_MARKER)) {
    return `capture rate limit exceeded (${RATE_LIMIT_MARKER}); retry shortly`;
  }

  const lower = text.toLowerCase();
  if (RESTRICTED_MARKERS.some((marker) => lower.includes(marker))) {
    return `cannot capture this page: ${text}`;
  }
  return `capture failed: ${text}`;
}
