import type { SnapElement, ViewportInfo } from "../shared/protocol.js";
import { effectiveInputType, isFormField, isSubmitLike } from "./forms.ts";

/** Minimal window surface used by the snapshot logic (keeps this module jsdom-friendly). */
export interface WindowLike {
  innerHeight: number;
  innerWidth?: number;
  scrollX?: number;
  scrollY: number;
}

export interface SnapOptions {
  all?: boolean;
  formsOnly?: boolean;
  /** Include cursor-pointer controls and bare rows/cells (opt-in: snapshot explosion). */
  deep?: boolean;
  /** Include elements inside frames regardless of frame size/visibility. */
  frames?: boolean;
  /** Cap on emitted elements; defaults to MAX_SNAP_ELEMENTS. */
  maxElements?: number;
  /** First id to assign; lets the caller keep ids unique across snaps. */
  startId?: number;
  /** Injectable hit-test (top-frame coordinates); tests use this instead of elementFromPoint. */
  hitTest?: (x: number, y: number) => Element | null;
}

/** Axis-aligned box in top-frame viewport CSS pixels. */
export interface ViewportRect {
  left: number;
  top: number;
  width: number;
  height: number;
}

/** Compose a nested rect with one frame's border box (+ its borders). Pure and unit-testable. */
export function addFrameOffset(
  rect: ViewportRect,
  frameRect: { left: number; top: number },
  clientLeft: number,
  clientTop: number,
): ViewportRect {
  return {
    left: rect.left + frameRect.left + clientLeft,
    top: rect.top + frameRect.top + clientTop,
    width: rect.width,
    height: rect.height,
  };
}

function frameElementOf(doc: Document): Element | null {
  try {
    return doc.defaultView?.frameElement ?? null;
  } catch {
    return null;
  }
}

/**
 * Element rect in top-frame viewport CSS pixels. Walks outward through nested
 * same-origin frames; shadow roots need no extra offset because their rects are
 * already relative to the containing document's viewport.
 */
export function viewportRect(el: Element): ViewportRect {
  const raw = el.getBoundingClientRect();
  let rect: ViewportRect = {
    left: raw.left,
    top: raw.top,
    width: raw.width,
    height: raw.height,
  };

  const seen = new Set<Element>();
  let frame = frameElementOf(el.ownerDocument);
  while (frame !== null && !seen.has(frame)) {
    seen.add(frame);
    rect = addFrameOffset(
      rect,
      frame.getBoundingClientRect(),
      frame.clientLeft ?? 0,
      frame.clientTop ?? 0,
    );
    frame = frameElementOf(frame.ownerDocument);
  }
  return rect;
}

export interface Snapshot {
  viewport: ViewportInfo;
  elements: SnapElement[];
  lines: string[];
  byId: Map<number, Element>;
  /** First id not used by this snapshot (monotonic across calls with startId). */
  nextId: number;
  /** Visible candidates dropped because the element cap was reached. */
  omitted: number;
  framesSummary?: string;
}

export const MAX_SNAP_ELEMENTS = 400;

const CANDIDATE_SELECTOR = [
  "a[href]",
  "button",
  "input",
  "textarea",
  "select",
  '[role="button"]',
  '[role="link"]',
  '[role="checkbox"]',
  '[role="menuitem"]',
  '[role="tab"]',
  '[role="combobox"]',
  '[role="listbox"]',
  '[role="option"]',
  '[role="menu"]',
  '[role="menuitemcheckbox"]',
  '[role="menuitemradio"]',
  '[role="switch"]',
  '[role="slider"]',
  '[role="searchbox"]',
  '[role="radio"]',
  '[role="treeitem"]',
  '[role="spinbutton"]',
  '[role="row"][aria-selected]',
  '[role="gridcell"][aria-selected]',
  '[role="gridcell"][tabindex]',
  '[tabindex]:not([tabindex="-1"])',
  "[aria-expanded]",
  "[aria-haspopup]",
  "summary",
  "[onclick]",
  '[contenteditable="true"]',
].join(",");

/** Bare structural rows/cells are opt-in: on data grids they explode the snapshot. */
const DEEP_CANDIDATE_SELECTOR = ['[role="row"]', '[role="gridcell"]'].join(",");

const NON_POINTER_CONTAINERS = new Set([
  "html",
  "body",
  "main",
  "section",
  "article",
  "nav",
]);

const VIEWPORT_MARGIN_PX = 100;
const MIN_RECT_PX = 4;
const LABEL_MAX_CHARS = 80;
const VALUE_MAX_CHARS = 40;

const ALWAYS_PARENS_TAGS = new Set(["input", "textarea", "select"]);

function computedStyle(doc: Document, el: Element): CSSStyleDeclaration | null {
  const view = doc.defaultView;
  if (!view) {
    return null;
  }
  try {
    return view.getComputedStyle(el);
  } catch {
    return null;
  }
}

/**
 * Layout and style are read at most once per element per snapshot. The cache is
 * scoped to one `snapshot()` call so callers never see stale geometry.
 */
interface Measurement {
  local: DOMRect;
  style: CSSStyleDeclaration | null;
}

type MeasureCache = Map<Element, Measurement>;

function measureElement(el: Element, cache: MeasureCache): Measurement {
  const cached = cache.get(el);
  if (cached !== undefined) {
    return cached;
  }
  const measurement: Measurement = {
    local: el.getBoundingClientRect(),
    style: computedStyle(el.ownerDocument, el),
  };
  cache.set(el, measurement);
  return measurement;
}

/** Composed viewport rect using cached local rects for the element and its frames. */
function composedRect(el: Element, cache: MeasureCache): ViewportRect {
  const local = measureElement(el, cache).local;
  let rect: ViewportRect = {
    left: local.left,
    top: local.top,
    width: local.width,
    height: local.height,
  };

  const seen = new Set<Element>();
  let frame = frameElementOf(el.ownerDocument);
  while (frame !== null && !seen.has(frame)) {
    seen.add(frame);
    const frameRect = measureElement(frame, cache).local;
    rect = addFrameOffset(
      rect,
      frameRect,
      frame.clientLeft ?? 0,
      frame.clientTop ?? 0,
    );
    frame = frameElementOf(frame.ownerDocument);
  }
  return rect;
}

export function collapseWhitespace(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

export function truncateText(value: string, maxChars: number): string {
  return value.length > maxChars ? `${value.slice(0, maxChars)}\u2026` : value;
}

function visibleText(el: Element): string {
  const maybeHtml = el as HTMLElement;
  if (typeof maybeHtml.innerText === "string" && maybeHtml.innerText.trim() !== "") {
    return collapseWhitespace(maybeHtml.innerText);
  }
  return collapseWhitespace(el.textContent);
}

function associatedLabelText(el: Element, doc: Document): string {
  const wrapping = el.closest("label");
  if (wrapping) {
    return visibleText(wrapping);
  }
  if (!el.id) {
    return "";
  }
  const labels = doc.querySelectorAll("label[for]");
  for (const label of Array.from(labels)) {
    if (label.getAttribute("for") === el.id) {
      return visibleText(label);
    }
  }
  return "";
}

function labelledByText(el: Element, doc: Document): string {
  const ids = collapseWhitespace(el.getAttribute("aria-labelledby"));
  if (!ids) {
    return "";
  }
  const parts: string[] = [];
  for (const id of ids.split(" ")) {
    const ref = doc.getElementById(id);
    if (ref) {
      const text = visibleText(ref);
      if (text) {
        parts.push(text);
      }
    }
  }
  return parts.join(" ");
}

/** Label precedence: aria-label, aria-labelledby, alt, placeholder, <label>, title, text, value. */
export function labelFor(el: Element, doc: Document): string {
  const ariaLabel = collapseWhitespace(el.getAttribute("aria-label"));
  if (ariaLabel) {
    return ariaLabel;
  }

  const labelledBy = labelledByText(el, doc);
  if (labelledBy) {
    return labelledBy;
  }

  const alt = collapseWhitespace(el.getAttribute("alt"));
  if (alt) {
    return alt;
  }

  const placeholder = collapseWhitespace(el.getAttribute("placeholder"));
  if (placeholder) {
    return placeholder;
  }

  const labelText = associatedLabelText(el, doc);
  if (labelText) {
    return labelText;
  }

  const title = collapseWhitespace(el.getAttribute("title"));
  if (title) {
    return title;
  }

  const text = visibleText(el);
  if (text) {
    return text;
  }

  if (isSubmitLike(el)) {
    const value = (el as HTMLInputElement).value;
    if (typeof value === "string") {
      return collapseWhitespace(value);
    }
  }

  return "";
}

/** Type token: link for anchors, effective input type, role, then explicit button type. */
export function typeToken(el: Element): string {
  const tag = el.tagName.toLowerCase();
  if (tag === "a") {
    return "link";
  }
  if (tag === "input") {
    return effectiveInputType(el);
  }
  const role = collapseWhitespace(el.getAttribute("role"));
  if (role) {
    return role;
  }
  if (tag === "button") {
    const type = el.getAttribute("type");
    if (type !== null && type.trim() !== "") {
      return type.trim().toLowerCase();
    }
  }
  return "";
}

function valueFor(el: Element): string {
  const tag = el.tagName.toLowerCase();
  if (tag !== "input" && tag !== "textarea" && tag !== "select") {
    return "";
  }
  const value = (el as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value;
  if (typeof value !== "string" || value === "") {
    return "";
  }
  return collapseWhitespace(value).slice(0, VALUE_MAX_CHARS);
}

export function stateTokens(el: Element): string[] {
  const tokens: string[] = [];
  const element = el as HTMLElement & {
    checked?: boolean;
    selected?: boolean;
    disabled?: boolean;
  };

  if (element.disabled === true || el.getAttribute("aria-disabled") === "true") {
    tokens.push("disabled");
  }

  const tag = el.tagName.toLowerCase();
  if (tag === "input") {
    const type = effectiveInputType(el);
    if ((type === "checkbox" || type === "radio") && element.checked === true) {
      tokens.push("checked");
    }
  }

  const role = collapseWhitespace(el.getAttribute("role")).toLowerCase();
  if (
    (role === "checkbox" || role === "radio" || role === "menuitemcheckbox") &&
    el.getAttribute("aria-checked") === "true"
  ) {
    tokens.push("checked");
  }

  if (el.getAttribute("aria-selected") === "true" || (tag === "option" && element.selected === true)) {
    tokens.push("selected");
  }

  const expanded = el.getAttribute("aria-expanded");
  if (expanded === "true") {
    tokens.push("expanded");
  } else if (expanded === "false") {
    tokens.push("collapsed");
  } else if (tag === "summary") {
    const parent = el.parentElement;
    if (parent !== null && parent.tagName.toLowerCase() === "details") {
      tokens.push((parent as HTMLDetailsElement).open === true ? "expanded" : "collapsed");
    }
  }

  if (tag === "input" && effectiveInputType(el) === "file") {
    tokens.push("file");
  }

  return tokens;
}

function isEmittedFormField(el: Element): boolean {
  const tag = el.tagName.toLowerCase();
  return (
    ALWAYS_PARENS_TAGS.has(tag) || el.getAttribute("contenteditable") === "true"
  );
}

function formatParens(value: string, state: string): string {
  if (value && state) {
    return ` (${value}, ${state})`;
  }
  if (value) {
    return ` (${value})`;
  }
  if (state) {
    return ` (${state})`;
  }
  return "";
}

export function formatLine(element: SnapElement, alwaysParens: boolean): string {
  const tagToken = element.type ? `${element.tag}:${element.type}` : element.tag;
  const escapedLabel = element.label.replace(/"/g, '\\"');
  const hasContent = element.value !== "" || element.state !== "";
  const parens = hasContent
    ? formatParens(element.value, element.state)
    : alwaysParens
      ? " ()"
      : "";
  const frameTag = element.frame ? ` (${element.frame})` : "";
  return `[${element.id}]${frameTag} <${tagToken}> "${escapedLabel}"${parens} @${element.x},${element.y}`;
}

/** Render snapshot lines after ids have been stabilised or remapped. */
export function formatSnapLines(
  elements: SnapElement[],
  byId: Map<number, Element>,
): string[] {
  return elements.map((element) => {
    const el = byId.get(element.id);
    return formatLine(element, el !== undefined && isEmittedFormField(el));
  });
}

function describeElement(
  el: Element,
  id: number,
  geometry: Element,
  frame: string | undefined,
  cache: MeasureCache,
): SnapElement {
  const rect = composedRect(geometry, cache);
  const ownerDoc = geometry.ownerDocument ?? el.ownerDocument;
  const result: SnapElement = {
    id,
    tag: el.tagName.toLowerCase(),
    type: typeToken(el),
    label: truncateText(labelFor(geometry === el ? el : geometry, ownerDoc), LABEL_MAX_CHARS),
    value: valueFor(el),
    state: stateTokens(el).join(", "),
    x: Math.round(rect.left),
    y: Math.round(rect.top),
    w: Math.round(rect.width),
    h: Math.round(rect.height),
  };
  if (frame) {
    result.frame = frame;
  }
  return result;
}

function shadowRootOf(el: Element): ShadowRoot | null {
  try {
    return el.shadowRoot ?? null;
  } catch {
    return null;
  }
}

export interface FrameCollectorContext {
  topDoc: Document;
  totalFrames: number;
  sameOriginTraversed: number;
  crossOrigin: number;
  skippedReasons: string[];
}

export function formatFramesSummary(ctx: FrameCollectorContext): string {
  if (ctx.totalFrames === 0) {
    return "";
  }
  const parts: string[] = [];
  parts.push(`${ctx.sameOriginTraversed} same-origin traversed`);
  if (ctx.crossOrigin > 0) {
    parts.push(`${ctx.crossOrigin} cross-origin`);
  }
  const skippedMap = new Map<string, number>();
  for (const reason of ctx.skippedReasons) {
    if (reason !== "cross-origin") {
      skippedMap.set(reason, (skippedMap.get(reason) ?? 0) + 1);
    }
  }
  if (skippedMap.size > 0) {
    const list: string[] = [];
    for (const [reason, count] of skippedMap) {
      list.push(count > 1 ? `${count} ${reason}` : reason);
    }
    parts.push(`skipped: ${list.join(", ")}`);
  }
  return `FRAMES: ${ctx.totalFrames} (${parts.join(", ")})`;
}

/** Same-origin iframe document and status. */
function frameDocumentOf(el: Element): { doc: Document | null; status: "same-origin" | "cross-origin" | "not-loaded" } {
  const tag = el.tagName.toLowerCase();
  if (tag !== "iframe" && tag !== "frame") {
    return { doc: null, status: "not-loaded" };
  }
  try {
    const doc = (el as HTMLIFrameElement).contentDocument ?? null;
    if (doc === null) {
      return { doc: null, status: "not-loaded" };
    }
    return { doc, status: "same-origin" };
  } catch {
    return { doc: null, status: "cross-origin" };
  }
}

/** Hidden or zero-size iframes are skipped entirely, including their contents. */
function isFrameVisible(frame: Element, cache: MeasureCache): boolean {
  const style = measureElement(frame, cache).style;
  if (
    style &&
    (style.display === "none" ||
      style.visibility === "hidden" ||
      style.visibility === "collapse")
  ) {
    return false;
  }
  const rect = measureElement(frame, cache).local;
  return rect.width >= MIN_RECT_PX && rect.height >= MIN_RECT_PX;
}

/** Opt-in heuristic for div-based controls: deepest element with cursor:pointer. */
function isPointerCandidate(el: Element, cache: MeasureCache): boolean {
  const tag = el.tagName.toLowerCase();
  if (NON_POINTER_CONTAINERS.has(tag)) {
    return false;
  }
  const style = measureElement(el, cache).style;
  if (style === null || style.cursor !== "pointer") {
    return false;
  }
  const parent = el.parentElement;
  if (parent !== null) {
    const parentStyle = measureElement(parent, cache).style;
    if (parentStyle !== null && parentStyle.cursor === "pointer") {
      return false;
    }
  }
  if (el.children.length > 6) {
    return false;
  }
  const text = collapseWhitespace(el.textContent).length;
  if (text > 160) {
    const label = collapseWhitespace(labelFor(el, el.ownerDocument));
    if (label === "") {
      return false;
    }
  }
  return true;
}

function matchesCandidate(el: Element, deep: boolean, cache: MeasureCache): boolean {
  if (!isCandidate(el)) {
    return false;
  }
  if (el.matches(CANDIDATE_SELECTOR)) {
    return true;
  }
  if (!deep) {
    return false;
  }
  if (el.matches(DEEP_CANDIDATE_SELECTOR)) {
    return true;
  }
  return isPointerCandidate(el, cache);
}

/** Composed parent crossing shadow roots and same-origin iframe boundaries. */
function composedParentNode(node: Node): Node | null {
  if (node.parentNode !== null) {
    return node.parentNode;
  }
  if (node.nodeType === 11) {
    return (node as ShadowRoot).host ?? null;
  }
  if (node.nodeType === 9) {
    return frameElementOf(node as Document);
  }
  return null;
}

/** `ancestor` contains `node` across shadow roots and same-origin frames. */
export function containsComposed(ancestor: Element, node: Element | null): boolean {
  let current: Node | null = node;
  for (let depth = 0; current !== null && depth < 512; depth += 1) {
    if (current === ancestor) {
      return true;
    }
    current = composedParentNode(current);
  }
  return false;
}

interface RectBox {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

function toRectBox(rect: ViewportRect): RectBox {
  return {
    left: rect.left,
    top: rect.top,
    right: rect.left + rect.width,
    bottom: rect.top + rect.height,
  };
}

function overlapsAtLeast(a: RectBox, b: RectBox, minimum: number): boolean {
  const width = Math.min(a.right, b.right) - Math.max(a.left, b.left);
  const height = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
  return width >= minimum && height >= minimum;
}

function ancestorFrames(el: Element): Element[] {
  const frames: Element[] = [];
  const seen = new Set<Document>();
  let doc = el.ownerDocument;
  for (let depth = 0; depth < 64; depth += 1) {
    if (seen.has(doc)) {
      break;
    }
    seen.add(doc);
    const frame = frameElementOf(doc);
    if (frame === null) {
      break;
    }
    frames.push(frame);
    doc = frame.ownerDocument;
  }
  return frames;
}

/** An iframe's content viewport in top-frame coordinates (borders excluded). */
function frameContentRect(frame: Element, cache: MeasureCache): RectBox {
  const rect = composedRect(frame, cache);
  const element = frame as HTMLElement;
  const borderLeft = frame.clientLeft ?? 0;
  const borderTop = frame.clientTop ?? 0;
  const width =
    element.clientWidth > 0
      ? element.clientWidth
      : Math.max(0, rect.width - borderLeft);
  const height =
    element.clientHeight > 0
      ? element.clientHeight
      : Math.max(0, rect.height - borderTop);
  return {
    left: rect.left + borderLeft,
    top: rect.top + borderTop,
    right: rect.left + borderLeft + width,
    bottom: rect.top + borderTop + height,
  };
}

/** Content scrolled outside any ancestor iframe's content box is clipped. */
export function isInsideFrameContent(
  el: Element,
  rect: ViewportRect,
  cache: MeasureCache = new Map(),
): boolean {
  const box = toRectBox(rect);
  for (const frame of ancestorFrames(el)) {
    if (!overlapsAtLeast(box, frameContentRect(frame, cache), MIN_RECT_PX)) {
      return false;
    }
  }
  return true;
}

/** Last visible modal in the top document, or null. `dialog[open]` or aria-modal. */
export function findVisibleModal(doc: Document): Element | null {
  let modal: Element | null = null;
  let candidates: Element[] = [];
  try {
    candidates = Array.from(doc.querySelectorAll('dialog[open], [aria-modal="true"]'));
  } catch {
    return null;
  }
  for (const el of candidates) {
    const style = computedStyle(doc, el);
    if (
      style &&
      (style.display === "none" ||
        style.visibility === "hidden" ||
        style.visibility === "collapse")
    ) {
      continue;
    }
    const rect = el.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0) {
      modal = el;
    }
  }
  return modal;
}

function isCulledByModal(
  doc: Document,
  el: Element,
  modal: Element,
  hitTest: (x: number, y: number) => Element | null,
  cache: MeasureCache,
): boolean {
  if (containsComposed(modal, el) || containsComposed(el, modal)) {
    return false;
  }
  const rect = composedRect(el, cache);
  const x = rect.left + rect.width / 2;
  const y = rect.top + rect.height / 2;
  let hit: Element | null = null;
  try {
    hit = hitTest(x, y);
  } catch {
    return false;
  }
  if (hit === null || hit === undefined) {
    return false;
  }
  return !containsComposed(el, hit) && !containsComposed(hit, el);
}

function isCulled(
  doc: Document,
  el: Element,
  win: WindowLike,
  all: boolean,
  modal: Element | null,
  hitTest: ((x: number, y: number) => Element | null) | null,
  cache: MeasureCache,
): boolean {
  const measured = measureElement(el, cache);
  const style = measured.style;
  if (style) {
    if (style.display === "none") {
      return true;
    }
    if (style.visibility === "hidden" || style.visibility === "collapse") {
      return true;
    }
    if (style.opacity !== "" && Number(style.opacity) === 0) {
      return true;
    }
  }

  const local = measured.local;
  if (local.width < MIN_RECT_PX || local.height < MIN_RECT_PX) {
    return true;
  }

  const rect = composedRect(el, cache);
  if (!all) {
    const viewportWidth = win.innerWidth ?? doc.documentElement?.clientWidth ?? 0;
    const viewportHeight = win.innerHeight;
    const outsideViewport = !(
      rect.top + rect.height >= -VIEWPORT_MARGIN_PX &&
      rect.top <= viewportHeight + VIEWPORT_MARGIN_PX &&
      rect.left + rect.width >= -VIEWPORT_MARGIN_PX &&
      rect.left <= viewportWidth + VIEWPORT_MARGIN_PX
    );
    if (outsideViewport || !isInsideFrameContent(el, rect, cache)) {
      return true;
    }
  }

  if (modal !== null && hitTest !== null) {
    return isCulledByModal(doc, el, modal, hitTest, cache);
  }
  return false;
}

function isCandidate(el: Element): boolean {
  if (el.tagName.toLowerCase() === "input") {
    return effectiveInputType(el) !== "hidden";
  }
  return true;
}

function isFileInput(el: Element): boolean {
  return el.tagName.toLowerCase() === "input" && effectiveInputType(el) === "file";
}

function isRendered(el: Element, cache: MeasureCache): boolean {
  const measured = measureElement(el, cache);
  const style = measured.style;
  if (
    style &&
    (style.display === "none" ||
      style.visibility === "hidden" ||
      style.visibility === "collapse")
  ) {
    return false;
  }
  const rect = measured.local;
  return rect.width >= MIN_RECT_PX && rect.height >= MIN_RECT_PX;
}

/**
 * A visually hidden file input is unreachable by clicking, so emit it at its
 * associated visible label's rect instead. Clicking that label opens the picker.
 */
function fileInputGeometry(el: Element, cache: MeasureCache): Element {
  if (!isFileInput(el) || isRendered(el, cache)) {
    return el;
  }
  const id = el.getAttribute("id");
  if (id !== null && id !== "") {
    const labelled = Array.from(el.ownerDocument.querySelectorAll("label[for]")).find(
      (label) => label.getAttribute("for") === id,
    );
    if (labelled !== undefined && isRendered(labelled, cache)) {
      return labelled;
    }
  }
  const wrapping = el.closest("label");
  if (wrapping !== null && isRendered(wrapping, cache)) {
    return wrapping;
  }
  return el;
}

function resolveWindow(doc: Document): WindowLike {
  const view = doc.defaultView;
  return {
    innerHeight: view?.innerHeight ?? 0,
    innerWidth: view?.innerWidth,
    scrollX: view?.scrollX ?? 0,
    scrollY: view?.scrollY ?? 0,
  };
}

export function computeViewport(doc: Document, win?: WindowLike): ViewportInfo {
  const windowLike = win ?? resolveWindow(doc);
  const scrollY = Number.isFinite(windowLike.scrollY) ? windowLike.scrollY : 0;
  const viewportHeight = Number.isFinite(windowLike.innerHeight) ? windowLike.innerHeight : 0;
  const totalHeight = doc.documentElement?.scrollHeight ?? 0;

  let percent = 0;
  if (totalHeight > 0) {
    percent = Math.round(((scrollY + viewportHeight) / totalHeight) * 100);
    percent = Math.max(0, Math.min(100, percent));
  }

  return { scrollY, viewportHeight, totalHeight, percent };
}

/**
 * Overlay containers index their children individually: a listbox must not
 * hide its options, a menu its items. Flattening still applies to leaf-like
 * controls such as a button wrapping a span.
 */
const CHILD_ADDRESSABLE_ROLES = new Set([
  "listbox",
  "menu",
  "menubar",
  "tree",
  "tablist",
  "grid",
  "radiogroup",
  "toolbar",
]);

function suppressesDescendants(el: Element): boolean {
  const role = el.getAttribute("role");
  return role === null || !CHILD_ADDRESSABLE_ROLES.has(role);
}

type WalkRoot = Document | ShadowRoot | Element;

interface SnapshotBuild {
  doc: Document;
  win: WindowLike;
  all: boolean;
  formsOnly: boolean;
  deep: boolean;
  forceFrames: boolean;
  maxElements: number;
  modal: Element | null;
  hitTest: ((x: number, y: number) => Element | null) | null;
  cache: MeasureCache;
  elements: SnapElement[];
  byId: Map<number, Element>;
  seen: Set<Element>;
  nextId: number;
  omitted: number;
  frames: FrameCollectorContext;
}

/** Traverse a same-origin frame at the frame element's position in the walk. */
function visitFrame(frame: Element, depth: number, ctx: SnapshotBuild): void {
  ctx.frames.totalFrames += 1;
  const { doc: frameDoc, status } = frameDocumentOf(frame);
  if (status === "cross-origin") {
    ctx.frames.crossOrigin += 1;
    ctx.frames.skippedReasons.push("cross-origin");
    return;
  }
  if (status === "not-loaded" || frameDoc === null) {
    ctx.frames.skippedReasons.push("not loaded");
    return;
  }
  if (!isFrameVisible(frame, ctx.cache) && !ctx.forceFrames) {
    const style = measureElement(frame, ctx.cache).style;
    if (
      style &&
      (style.display === "none" ||
        style.visibility === "hidden" ||
        style.visibility === "collapse")
    ) {
      ctx.frames.skippedReasons.push("hidden");
    } else {
      ctx.frames.skippedReasons.push("zero-size");
    }
    return;
  }

  ctx.frames.sameOriginTraversed += 1;
  const frameTag = `f${ctx.frames.sameOriginTraversed}`;
  // A frame document starts a new tree: emitted ancestors outside the frame do
  // not suppress its contents (matching the old composed-parent walk).
  walkComposed(frameDoc, false, frameTag, depth + 1, ctx);
}

/**
 * One composed-tree pass: light DOM in document order, each open shadow root at
 * its host's position, then same-origin frame documents at the frame's
 * position. `blocked` replaces the old per-element ancestor walk: an emitted
 * element that suppresses descendants blocks the rest of its subtree.
 *
 * The walk is iterative so light-DOM nesting is unbounded (the old flat
 * `querySelectorAll` had no recursion cost per level). `depth` counts composed
 * boundaries only — shadow roots and frames — and stays capped at 64 like the
 * original guard.
 */
type WorkItem =
  | {
      kind: "nodes";
      nodes: Element[];
      index: number;
      blocked: boolean;
      frameTag: string | undefined;
      depth: number;
    }
  | { kind: "frame"; el: Element; depth: number };

const MAX_COMPOSED_DEPTH = 64;

function walkComposed(
  root: WalkRoot,
  blocked: boolean,
  frameTag: string | undefined,
  depth: number,
  ctx: SnapshotBuild,
): void {
  if (depth > MAX_COMPOSED_DEPTH) {
    return;
  }

  const stack: WorkItem[] = [
    {
      kind: "nodes",
      nodes: Array.from(root.children),
      index: 0,
      blocked,
      frameTag,
      depth,
    },
  ];

  while (stack.length > 0) {
    const item = stack[stack.length - 1];

    if (item.kind === "frame") {
      stack.pop();
      visitFrame(item.el, item.depth, ctx);
      continue;
    }

    if (item.index >= item.nodes.length) {
      stack.pop();
      continue;
    }
    const el = item.nodes[item.index];
    item.index += 1;

    if (ctx.seen.has(el)) {
      continue;
    }
    ctx.seen.add(el);

    let childBlocked = item.blocked;
    if (!item.blocked && matchesCandidate(el, ctx.deep, ctx.cache)) {
      const eligible = !ctx.formsOnly || isFormField(el);
      if (eligible) {
        const geometry = fileInputGeometry(el, ctx.cache);
        if (
          !isCulled(
            ctx.doc,
            geometry,
            ctx.win,
            ctx.all,
            ctx.modal,
            ctx.hitTest,
            ctx.cache,
          )
        ) {
          if (ctx.elements.length >= ctx.maxElements) {
            ctx.omitted += 1;
          } else {
            const id = ctx.nextId;
            ctx.nextId += 1;
            const described = describeElement(el, id, geometry, item.frameTag, ctx.cache);
            ctx.elements.push(described);
            ctx.byId.set(id, el);
            if (suppressesDescendants(el)) {
              childBlocked = true;
            }
          }
        }
      }
    }

    const tag = el.tagName.toLowerCase();
    const shadow = shadowRootOf(el);
    const boundaryDepth = item.depth + 1;

    // Push in reverse execution order: light children run last, the frame is
    // traversed before them, and the shadow root runs immediately after `el`.
    stack.push({
      kind: "nodes",
      nodes: Array.from(el.children),
      index: 0,
      blocked: childBlocked,
      frameTag: item.frameTag,
      depth: item.depth,
    });
    if (tag === "iframe" || tag === "frame") {
      stack.push({ kind: "frame", el, depth: item.depth });
    }
    if (shadow !== null && boundaryDepth <= MAX_COMPOSED_DEPTH) {
      stack.push({
        kind: "nodes",
        nodes: Array.from(shadow.children),
        index: 0,
        blocked: childBlocked,
        frameTag: item.frameTag,
        depth: boundaryDepth,
      });
    }
  }
}

/**
 * Build the interactive snapshot of a document. Pure: no chrome.* access and no
 * import-time DOM reads, so it can run unmodified under jsdom.
 */
export function snapshot(doc: Document, options: SnapOptions = {}, win?: WindowLike): Snapshot {
  const windowLike = win ?? resolveWindow(doc);
  const maxElements =
    typeof options.maxElements === "number" && Number.isFinite(options.maxElements) && options.maxElements > 0
      ? Math.floor(options.maxElements)
      : MAX_SNAP_ELEMENTS;
  const nextId =
    typeof options.startId === "number" && Number.isFinite(options.startId)
      ? Math.max(1, Math.floor(options.startId))
      : 1;

  const modal = findVisibleModal(doc);
  const hitTest =
    options.hitTest ??
    (typeof doc.elementFromPoint === "function"
      ? (x: number, y: number) => doc.elementFromPoint(x, y)
      : null);

  const ctx: SnapshotBuild = {
    doc,
    win: windowLike,
    all: options.all === true,
    formsOnly: options.formsOnly === true,
    deep: options.deep === true,
    forceFrames: options.frames === true,
    maxElements,
    modal,
    hitTest,
    cache: new Map(),
    elements: [],
    byId: new Map(),
    seen: new Set(),
    nextId,
    omitted: 0,
    frames: {
      topDoc: doc,
      totalFrames: 0,
      sameOriginTraversed: 0,
      crossOrigin: 0,
      skippedReasons: [],
    },
  };

  walkComposed(doc, false, undefined, 0, ctx);

  const framesSummary = formatFramesSummary(ctx.frames);

  return {
    viewport: computeViewport(doc, windowLike),
    elements: ctx.elements,
    lines: formatSnapLines(ctx.elements, ctx.byId),
    byId: ctx.byId,
    nextId: ctx.nextId,
    omitted: ctx.omitted,
    ...(framesSummary ? { framesSummary } : {}),
  };
}
