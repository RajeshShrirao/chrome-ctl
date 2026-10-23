/**
 * Phase 5 descriptor capture and tiered target resolution.
 *
 * Pure DOM functions (no chrome.* access, no import-time DOM reads) so the
 * deterministic descriptor helpers stay testable under jsdom. Resolution
 * searches the light DOM, open shadow roots, and same-origin iframes.
 */

import type { MacroTarget, PageResolveResult, ResolveTier } from "../shared/protocol.js";
import { typeToken, viewportRect } from "./dom.ts";

const TEST_ID_ATTRIBUTES = ["data-testid", "data-test", "data-qa", "data-cy"] as const;
const MAX_NAME_CHARS = 80;
const MAX_TEXT_CHARS = 80;
const MAX_CSS_ANCESTORS = 4;

export const DEFAULT_RESOLVE_TIMEOUT_MS = 3_000;
export const MIN_RESOLVE_TIMEOUT_MS = 250;
export const MAX_RESOLVE_TIMEOUT_MS = 15_000;
const RESOLVE_POLL_MS = 150;

const TEXT_CANDIDATE_SELECTOR =
  'button, a, summary, label, [role="button"], [role="link"]';
const ARIA_CANDIDATE_SELECTOR =
  'button, a[href], input, textarea, select, summary, label, [role]';

/** Collapse runs of whitespace and trim; the canonical text form everywhere. */
export function normalizeText(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

function visibleText(el: Element): string {
  const maybeHtml = el as HTMLElement;
  if (typeof maybeHtml.innerText === "string") {
    const inner = normalizeText(maybeHtml.innerText);
    if (inner) {
      return inner;
    }
  }
  return normalizeText(el.textContent);
}

function wrappingLabelText(el: Element): string {
  const wrapping = el.closest("label");
  return wrapping ? visibleText(wrapping) : "";
}

function labelledByText(el: Element, doc: Document): string {
  const ids = normalizeText(el.getAttribute("aria-labelledby"));
  if (!ids) {
    return "";
  }
  const parts: string[] = [];
  for (const id of ids.split(" ")) {
    const ref = doc.getElementById(id);
    const text = ref ? visibleText(ref) : "";
    if (text) {
      parts.push(text);
    }
  }
  return parts.join(" ");
}

function associatedLabelText(el: Element, doc: Document): string {
  const wrapping = wrappingLabelText(el);
  if (wrapping) {
    return wrapping;
  }
  const id = el.getAttribute("id");
  if (!id) {
    return "";
  }
  for (const label of Array.from(doc.querySelectorAll("label[for]"))) {
    if (label.getAttribute("for") === id) {
      return visibleText(label);
    }
  }
  return "";
}

/** Accessible-name approximation shared by descriptors and ariaLabel matching. */
export function accessibleName(el: Element, doc: Document): string {
  const direct = normalizeText(el.getAttribute("aria-label"));
  if (direct) {
    return direct.slice(0, MAX_NAME_CHARS);
  }

  const labelledBy = labelledByText(el, doc);
  if (labelledBy) {
    return labelledBy.slice(0, MAX_NAME_CHARS);
  }

  const label = associatedLabelText(el, doc);
  if (label) {
    return label.slice(0, MAX_NAME_CHARS);
  }

  for (const attribute of ["placeholder", "alt", "title"]) {
    const value = normalizeText(el.getAttribute(attribute));
    if (value) {
      return value.slice(0, MAX_NAME_CHARS);
    }
  }

  return visibleText(el).slice(0, MAX_NAME_CHARS);
}

/** Explicit role when present, otherwise the implicit HTML role. */
export function roleFor(el: Element): string {
  const explicit = normalizeText(el.getAttribute("role")).toLowerCase();
  if (explicit) {
    return explicit;
  }

  const tag = el.tagName.toLowerCase();
  if (tag === "button") {
    return "button";
  }
  if (tag === "a" && el.hasAttribute("href")) {
    return "link";
  }
  if (tag === "input") {
    const type = (el.getAttribute("type") ?? "").trim().toLowerCase();
    if (type === "checkbox") {
      return "checkbox";
    }
    if (type === "radio") {
      return "radio";
    }
    return "textbox";
  }
  if (tag === "textarea") {
    return "textbox";
  }
  if (tag === "select") {
    return "combobox";
  }
  return "";
}

/** First non-empty authored test id, in stable precedence order. */
export function testIdFor(el: Element): string {
  for (const attribute of TEST_ID_ATTRIBUTES) {
    const value = normalizeText(el.getAttribute(attribute));
    if (value) {
      return value;
    }
  }
  return "";
}

function siblingIndex(el: Element): number {
  let index = 1;
  for (
    let sibling = el.previousElementSibling;
    sibling !== null;
    sibling = sibling.previousElementSibling
  ) {
    if (sibling.tagName === el.tagName) {
      index += 1;
    }
  }
  return index;
}

/** Positional XPath anchored at `<body>`, or `//*[@id="..."]` when ids allow it. */
export function xpathFor(el: Element): string {
  const id = el.getAttribute("id");
  if (id && !/["'\r\n]/.test(id)) {
    return `//*[@id="${id}"]`;
  }

  const segments: string[] = [];
  let current: Element | null = el;
  while (current !== null) {
    const tag = current.tagName.toLowerCase();
    if (tag === "body") {
      segments.unshift("body");
      break;
    }
    if (tag === "html") {
      break;
    }
    segments.unshift(`${tag}[${siblingIndex(current)}]`);
    current = current.parentElement;
  }

  if (segments.length === 0 || segments[0] !== "body") {
    return "";
  }
  return `//${segments.join("/")}`;
}

/**
 * CSS.escape with a spec-compatible fallback (jsdom and older workers do not
 * ship `CSS.escape`).
 */
export function cssEscape(value: string): string {
  const css = (globalThis as { CSS?: { escape?: (input: string) => string } }).CSS;
  if (css && typeof css.escape === "function") {
    return css.escape(value);
  }

  let result = "";
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    const char = value.charAt(index);
    if (code === 0x0000) {
      result += "\uFFFD";
      continue;
    }
    if ((code >= 0x0001 && code <= 0x001f) || code === 0x007f) {
      result += `\\${code.toString(16)} `;
      continue;
    }
    if (index === 0 && code >= 0x0030 && code <= 0x0039) {
      result += `\\${code.toString(16)} `;
      continue;
    }
    if (
      index === 1 &&
      code >= 0x0030 &&
      code <= 0x0039 &&
      value.charCodeAt(0) === 0x002d
    ) {
      result += `\\${code.toString(16)} `;
      continue;
    }
    if (
      code >= 0x0080 ||
      code === 0x002d ||
      code === 0x005f ||
      (code >= 0x0030 && code <= 0x0039) ||
      (code >= 0x0041 && code <= 0x005a) ||
      (code >= 0x0061 && code <= 0x007a)
    ) {
      result += char;
      continue;
    }
    result += `\\${char}`;
  }
  return result;
}

function looksAuthoredId(id: string): boolean {
  if (id === "") {
    return false;
  }
  if (/^\d+$/.test(id)) {
    return false;
  }
  if (/^[0-9a-f]{8,}$/i.test(id)) {
    return false;
  }
  if (/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id)) {
    return false;
  }
  return true;
}

function nthOfTypeSegment(el: Element): string {
  return `${el.tagName.toLowerCase()}:nth-of-type(${siblingIndex(el)})`;
}

function nthOfTypeChain(el: Element): string {
  const segments: string[] = [nthOfTypeSegment(el)];
  let ancestors = 0;
  for (
    let parent = el.parentElement;
    parent !== null && ancestors < MAX_CSS_ANCESTORS;
    parent = parent.parentElement
  ) {
    const tag = parent.tagName.toLowerCase();
    if (tag === "html" || tag === "body") {
      break;
    }
    segments.unshift(nthOfTypeSegment(parent));
    ancestors += 1;
  }
  return segments.join(" > ");
}

/** Stable CSS selector: authored id, test id, form name, then `nth-of-type`. */
export function cssSelectorFor(el: Element): string {
  const id = el.getAttribute("id") ?? "";
  if (looksAuthoredId(id)) {
    return `#${cssEscape(id)}`;
  }

  for (const attribute of TEST_ID_ATTRIBUTES) {
    const value = normalizeText(el.getAttribute(attribute));
    if (value) {
      return `[${attribute}="${cssEscape(value)}"]`;
    }
  }

  const tag = el.tagName.toLowerCase();
  if (tag === "input" || tag === "select" || tag === "textarea" || tag === "button") {
    const name = normalizeText(el.getAttribute("name"));
    if (name) {
      return `[name="${cssEscape(name)}"]`;
    }
  }

  return nthOfTypeChain(el);
}

/** Deterministic multi-tier descriptor for an arbitrary element. */
export function buildDescriptor(doc: Document, el: Element): MacroTarget {
  const target: MacroTarget = {};
  const ownerDoc = ownerDocumentOf(el, doc);

  const testId = testIdFor(el);
  if (testId) {
    target.testId = testId;
  }

  const name = accessibleName(el, ownerDoc);
  if (name) {
    target.ariaLabel = name;
  }

  const role = roleFor(el);
  if (role) {
    target.role = role;
  }

  const text = visibleText(el);
  if (text && text.length <= MAX_TEXT_CHARS) {
    target.text = text;
  }

  const tag = el.tagName.toLowerCase();
  target.tag = tag;
  if (tag === "input") {
    const type = (el.getAttribute("type") ?? "").trim().toLowerCase();
    if (type) {
      target.type = type;
    }
  }

  const xpath = xpathFor(el);
  if (xpath) {
    target.xpath = xpath;
  }

  const cssSelector = cssSelectorFor(el);
  if (cssSelector) {
    target.cssSelector = cssSelector;
  }

  return target;
}

function clampResolveTimeout(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return DEFAULT_RESOLVE_TIMEOUT_MS;
  }
  return Math.max(
    MIN_RESOLVE_TIMEOUT_MS,
    Math.min(MAX_RESOLVE_TIMEOUT_MS, Math.round(value)),
  );
}

function sleep(ms: number): Promise<void> {
  const delay = Math.max(0, Math.round(ms));
  if (delay === 0) {
    return Promise.resolve();
  }
  return new Promise((resolve) => setTimeout(resolve, delay));
}

type QueryRoot = Document | ShadowRoot;

/** Every document root reachable from `doc`: itself, open shadow roots, same-origin frames. */
function collectDeepRoots(
  root: QueryRoot,
  out: QueryRoot[],
  seenDocs: Set<Document>,
  depth: number,
): void {
  if (depth > 64) {
    return;
  }
  out.push(root);

  for (const el of Array.from(root.querySelectorAll("*"))) {
    let shadow: ShadowRoot | null = null;
    try {
      shadow = el.shadowRoot;
    } catch {
      shadow = null;
    }
    if (shadow) {
      collectDeepRoots(shadow, out, seenDocs, depth + 1);
    }

    if (el.tagName.toLowerCase() !== "iframe") {
      continue;
    }
    let frameDoc: Document | null = null;
    try {
      frameDoc = (el as HTMLIFrameElement).contentDocument;
    } catch {
      frameDoc = null;
    }
    if (frameDoc && !seenDocs.has(frameDoc)) {
      seenDocs.add(frameDoc);
      collectDeepRoots(frameDoc, out, seenDocs, depth + 1);
    }
  }
}

/** All query roots under the top document, in traversal order. */
export function deepRoots(doc: Document): QueryRoot[] {
  const roots: QueryRoot[] = [];
  const seenDocs = new Set<Document>([doc]);
  collectDeepRoots(doc, roots, seenDocs, 0);
  return roots;
}

/** `querySelectorAll` evaluated against every deep root, deduplicated. */
export function queryDeepAll(doc: Document, selector: string): Element[] {
  const matches: Element[] = [];
  const seen = new Set<Element>();
  for (const root of deepRoots(doc)) {
    let found: Element[];
    try {
      found = Array.from(root.querySelectorAll(selector));
    } catch {
      continue;
    }
    for (const el of found) {
      if (!seen.has(el)) {
        seen.add(el);
        matches.push(el);
      }
    }
  }
  return matches;
}

function ownerDocumentOf(el: Element, fallback: Document): Document {
  return el.ownerDocument ?? fallback;
}

function isDisabled(el: Element): boolean {
  const control = el as HTMLElement & { disabled?: boolean };
  return control.disabled === true || el.getAttribute("aria-disabled") === "true";
}

/** Visible, enabled, and sized after centering the element in the viewport. */
function isUsable(el: Element): boolean {
  if (!el.isConnected || isDisabled(el)) {
    return false;
  }
  if (el.getClientRects().length === 0) {
    return false;
  }
  try {
    el.scrollIntoView({ block: "center" });
  } catch {
    // Non-fatal: measurement below still reflects the current layout.
  }
  const rect = el.getBoundingClientRect();
  return rect.width > 0 && rect.height > 0;
}

function findVisible(elements: Element[]): Element | null {
  for (const el of elements) {
    if (isUsable(el)) {
      return el;
    }
  }
  return null;
}

function findByTestId(doc: Document, testId: string): Element | null {
  for (const attribute of TEST_ID_ATTRIBUTES) {
    const matches: Element[] = [];
    for (const el of queryDeepAll(doc, `[${attribute}]`)) {
      if (el.getAttribute(attribute) === testId) {
        matches.push(el);
      }
    }
    const hit = findVisible(matches);
    if (hit) {
      return hit;
    }
  }
  return null;
}

function implicitSelectorFor(role: string): string {
  switch (role) {
    case "button":
      return 'button, input[type="button"], input[type="submit"], input[type="reset"], [role="button"]';
    case "link":
      return 'a[href], [role="link"]';
    case "checkbox":
      return 'input[type="checkbox"], [role="checkbox"]';
    case "radio":
      return 'input[type="radio"], [role="radio"]';
    case "textbox":
      return 'input, textarea, [role="textbox"]';
    case "combobox":
      return 'select, [role="combobox"]';
    default:
      return `[role="${cssEscape(role)}"]`;
  }
}

function findByAriaLabel(
  doc: Document,
  ariaLabel: string,
  role: string | undefined,
): Element | null {
  const expected = normalizeText(ariaLabel).slice(0, MAX_NAME_CHARS);
  if (!expected) {
    return null;
  }
  const normalizedRole = normalizeText(role).toLowerCase();
  const selector = normalizedRole
    ? implicitSelectorFor(normalizedRole)
    : ARIA_CANDIDATE_SELECTOR;

  for (const el of queryDeepAll(doc, selector)) {
    if (accessibleName(el, ownerDocumentOf(el, doc)) === expected && isUsable(el)) {
      return el;
    }
  }
  return null;
}

function findByText(doc: Document, text: string): Element | null {
  const expected = normalizeText(text);
  if (!expected) {
    return null;
  }

  const candidates = queryDeepAll(doc, TEXT_CANDIDATE_SELECTOR);
  for (const el of candidates) {
    if (visibleText(el) === expected && isUsable(el)) {
      return el;
    }
  }

  const lower = expected.toLowerCase();
  for (const el of candidates) {
    if (visibleText(el).toLowerCase().includes(lower) && isUsable(el)) {
      return el;
    }
  }
  return null;
}

function findByXpath(doc: Document, xpath: string): Element | null {
  // XPathResult.FIRST_ORDERED_NODE_TYPE === 9; the numeric constant works in
  // workers and jsdom realms that do not expose the constructor globally.
  // Shadow trees are invisible to XPath, so evaluate once per owning document.
  const seenDocs = new Set<Document>();
  for (const root of deepRoots(doc)) {
    const owner =
      root.nodeType === 9
        ? (root as Document)
        : (root.ownerDocument ?? doc);
    if (seenDocs.has(owner)) {
      continue;
    }
    seenDocs.add(owner);
    try {
      const result = owner.evaluate(xpath, owner, null, 9, null);
      const node = result.singleNodeValue;
      if (node !== null && node.nodeType === 1 && isUsable(node as Element)) {
        return node as Element;
      }
    } catch {
      // Invalid XPath or an environment without XPath support.
    }
  }
  return null;
}

function findByCssSelector(doc: Document, selector: string): Element | null {
  for (const el of queryDeepAll(doc, selector)) {
    if (isUsable(el)) {
      return el;
    }
  }
  return null;
}

export interface TargetMatch {
  element: Element;
  tier: ResolveTier;
}

/** Single pass over the tiers in contract order. */
export function findTargetOnce(doc: Document, target: MacroTarget): TargetMatch | null {
  if (target.testId) {
    const el = findByTestId(doc, target.testId);
    if (el) {
      return { element: el, tier: "testId" };
    }
  }

  if (target.ariaLabel) {
    const el = findByAriaLabel(doc, target.ariaLabel, target.role);
    if (el) {
      return { element: el, tier: "ariaLabel" };
    }
  }

  if (target.text) {
    const el = findByText(doc, target.text);
    if (el) {
      return { element: el, tier: "text" };
    }
  }

  if (target.xpath) {
    const el = findByXpath(doc, target.xpath);
    if (el) {
      return { element: el, tier: "xpath" };
    }
  }

  if (target.cssSelector) {
    const el = findByCssSelector(doc, target.cssSelector);
    if (el) {
      return { element: el, tier: "cssSelector" };
    }
  }

  return null;
}

/** `max(existing ids or 0) + 1`, so resolved ids never shadow snapshot ids. */
export function nextIndexId(index: Map<number, Element>): number {
  let max = 0;
  for (const key of index.keys()) {
    if (key > max) {
      max = key;
    }
  }
  return max + 1;
}

function describeTarget(target: MacroTarget): string {
  const parts: string[] = [];
  for (const key of [
    "testId",
    "ariaLabel",
    "text",
    "xpath",
    "cssSelector",
  ] as const) {
    const value = target[key];
    if (typeof value === "string" && value !== "") {
      parts.push(`${key}=${JSON.stringify(value.slice(0, 60))}`);
    }
  }
  return parts.length > 0 ? parts.join(", ") : "no selector provided";
}

function buildResolveResult(
  doc: Document,
  el: Element,
  id: number,
  tier: ResolveTier,
): PageResolveResult {
  const rect = viewportRect(el);
  return {
    id,
    tag: el.tagName.toLowerCase(),
    type: typeToken(el),
    label: accessibleName(el, ownerDocumentOf(el, doc)),
    x: Math.round(rect.left),
    y: Math.round(rect.top),
    w: Math.round(rect.width),
    h: Math.round(rect.height),
    matchedTier: tier,
  };
}

/**
 * Poll every 150 ms until the deadline because SPAs render late. On success the
 * element is registered under a fresh id so click/type/upload can use it.
 */
export async function resolveTarget(
  doc: Document,
  index: Map<number, Element>,
  target: MacroTarget,
  timeoutMs?: number,
): Promise<PageResolveResult> {
  const timeout = clampResolveTimeout(timeoutMs);
  const deadline = Date.now() + timeout;

  for (;;) {
    const match = findTargetOnce(doc, target);
    if (match) {
      const id = nextIndexId(index);
      index.set(id, match.element);
      return buildResolveResult(doc, match.element, id, match.tier);
    }

    const remaining = deadline - Date.now();
    if (remaining <= 0) {
      break;
    }
    await sleep(Math.min(RESOLVE_POLL_MS, remaining));
  }

  throw new Error(`element not found (${describeTarget(target)})`);
}
