/**
 * Phase 3 content helpers: target resolution, focus, metrics, submit-state and
 * waits. Pure DOM functions (no chrome.* access, no import-time DOM reads) so
 * they can be exercised directly under jsdom.
 */

import type {
  ContentAttrPayload,
  ContentStateEntry,
  ContentTargetPayload,
  ContentWaitPayload,
  ContentMetricsPayload,
} from "../shared/content.ts";
import { effectiveInputType } from "./forms.ts";
import { resolveFramePath, roleFor } from "./describe.ts";
import {
  computeViewport,
  containsComposed,
  isInsideFrameContent,
  labelFor,
  stateTokens,
  truncateText,
  typeToken,
  viewportRect,
  type ViewportRect,
  type WindowLike,
} from "./dom.ts";

const LABEL_MAX_CHARS = 80;
const MIN_INTERACTABLE_PX = 1;
const WAIT_DEFAULT_MS = 10_000;
const WAIT_MAX_MS = 30_000;
const IDLE_SETTLE_MS = 600;
const READBACK_MAX_CHARS = 1_000;

const NON_EDITABLE_INPUT_TYPES = new Set([
  "hidden",
  "checkbox",
  "radio",
  "submit",
  "button",
  "reset",
  "image",
  "file",
  "range",
  "color",
]);

const SUBMIT_INPUT_TYPES = new Set(["submit", "button", "image", "reset"]);

export interface RectLike {
  left: number;
  top: number;
  right: number;
  bottom: number;
  width: number;
  height: number;
}

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

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function missingElementMessage(id: number): string {
  return `element [${id}] not found; run snap first`;
}

/** Look up a snapshot id, rejecting detached elements. */
export function resolveIndexedElement(
  index: Map<number, Element> | undefined,
  id: number,
): Element {
  const element = index?.get(id);
  if (!element || !element.isConnected) {
    throw new Error(missingElementMessage(id));
  }
  return element;
}

/** Textarea, editable input, or `contenteditable="true"`. */
export function isEditableElement(el: Element): boolean {
  const tag = el.tagName.toLowerCase();
  if (tag === "textarea") {
    return true;
  }
  if (tag === "input") {
    return !NON_EDITABLE_INPUT_TYPES.has(effectiveInputType(el));
  }
  return el.getAttribute("contenteditable") === "true";
}

function nthOfTypeSegment(el: Element): string {
  const tag = el.tagName.toLowerCase();
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
  return `${tag}:nth-of-type(${index})`;
}

/** Unique structural path anchored at `<body>`, e.g. `body > div:nth-of-type(2) > input:nth-of-type(3)`. */
export function cssPathFor(el: Element): string {
  const segments: string[] = [];
  let current: Element | null = el;

  while (current !== null) {
    if (current.tagName.toLowerCase() === "body") {
      segments.unshift("body");
      break;
    }
    segments.unshift(nthOfTypeSegment(current));
    current = current.parentElement;
  }

  return segments.length > 0 ? segments.join(" > ") : "";
}

function viewportSize(doc: Document): { width: number; height: number } {
  const view = doc.defaultView;
  const width =
    typeof view?.innerWidth === "number" && view.innerWidth > 0
      ? view.innerWidth
      : (doc.documentElement?.clientWidth ?? 0);
  const height =
    typeof view?.innerHeight === "number" && view.innerHeight > 0
      ? view.innerHeight
      : (doc.documentElement?.clientHeight ?? 0);
  return { width, height };
}

function isFullyInViewport(rect: RectLike, viewport: { width: number; height: number }): boolean {
  return (
    rect.left >= 0 &&
    rect.top >= 0 &&
    rect.right <= viewport.width &&
    rect.bottom <= viewport.height
  );
}

function intersectsViewport(rect: RectLike, viewport: { width: number; height: number }): boolean {
  return (
    rect.right > 0 &&
    rect.bottom > 0 &&
    rect.left < viewport.width &&
    rect.top < viewport.height
  );
}

/** Best-effort scrollIntoView; jsdom and some SVG roots do not implement it. */
export function scrollIntoViewIfNeeded(el: Element, inline: boolean): void {
  const candidate = el as Element & {
    scrollIntoView?: (options?: ScrollIntoViewOptions | boolean) => void;
  };
  if (typeof candidate.scrollIntoView !== "function") {
    return;
  }
  try {
    candidate.scrollIntoView(
      inline ? { block: "center", inline: "center" } : { block: "center" },
    );
  } catch {
    // Non-fatal: measurement below still yields a usable (possibly clipped) rect.
  }
}

function isShadowAncestor(ancestor: Element, descendant: Element): boolean {
  return containsComposed(ancestor, descendant);
}

function isInShadowOf(descendant: Element, ancestor: Element): boolean {
  return containsComposed(ancestor, descendant);
}

/**
 * Hit-test the element's center piercing shadow roots and same-origin frames.
 * Returns `null` when the environment cannot answer (jsdom), in which case
 * obscuring cannot be detected.
 */
function hitTestPiercing(
  topDoc: Document,
  el: Element,
  cx: number,
  cy: number,
  viewport: { width: number; height: number },
): boolean | null {
  if (cx < 0 || cy < 0 || cx > viewport.width || cy > viewport.height) {
    return null;
  }
  let currentDoc: Document = topDoc;
  let currentX = cx;
  let currentY = cy;
  let hit: Element | null = null;
  const seenFrames = new Set<Element>();

  for (let depth = 0; depth < 16; depth += 1) {
    const fn = currentDoc.elementFromPoint;
    if (typeof fn !== "function") {
      return null;
    }
    let candidate: Element | null = null;
    try {
      candidate = fn.call(currentDoc, currentX, currentY);
    } catch {
      break;
    }
    if (!candidate) {
      break;
    }
    hit = candidate;

    while (
      hit &&
      (hit as Element).shadowRoot &&
      typeof (hit as Element).shadowRoot?.elementFromPoint === "function"
    ) {
      try {
        const inner = (hit as Element).shadowRoot!.elementFromPoint(currentX, currentY);
        if (!inner || inner === hit) {
          break;
        }
        hit = inner;
      } catch {
        break;
      }
    }

    const tag = hit.tagName.toLowerCase();
    if (tag === "iframe" || tag === "frame") {
      if (seenFrames.has(hit)) {
        break;
      }
      seenFrames.add(hit);
      let subDoc: Document | null = null;
      try {
        subDoc = (hit as HTMLIFrameElement).contentDocument ?? null;
      } catch {
        subDoc = null;
      }
      if (!subDoc) {
        break;
      }
      const frameRect = hit.getBoundingClientRect();
      const clientLeft = (hit as HTMLElement).clientLeft ?? 0;
      const clientTop = (hit as HTMLElement).clientTop ?? 0;
      currentX = currentX - frameRect.left - clientLeft;
      currentY = currentY - frameRect.top - clientTop;
      currentDoc = subDoc;
      continue;
    }

    break;
  }

  if (!hit) {
    return null;
  }
  return (
    hit === el ||
    el.contains(hit) ||
    hit.contains(el) ||
    isShadowAncestor(el, hit) ||
    isInShadowOf(el, hit)
  );
}

interface Interactability {
  interactable: boolean;
  reason: string;
}

function describeInteractability(
  doc: Document,
  el: Element,
  rect: ViewportRect,
): Interactability {
  if (!el.isConnected) {
    return { interactable: false, reason: "detached" };
  }

  const ownerDoc = el.ownerDocument ?? doc;
  const style = computedStyle(ownerDoc, el);
  if (
    style &&
    (style.display === "none" ||
      style.visibility === "hidden" ||
      style.visibility === "collapse")
  ) {
    return { interactable: false, reason: "hidden" };
  }

  const localRect = el.getBoundingClientRect();
  if (localRect.width < MIN_INTERACTABLE_PX || localRect.height < MIN_INTERACTABLE_PX) {
    return { interactable: false, reason: "zero size" };
  }

  if (style && style.pointerEvents === "none") {
    return { interactable: false, reason: "pointer-events: none" };
  }

  if (!isInsideFrameContent(el, rect)) {
    return { interactable: false, reason: "frame-clipped" };
  }

  const viewport = viewportSize(doc);
  let measured = rect;
  const rectLike = toRectLike(measured);
  if (!isFullyInViewport(rectLike, viewport)) {
    scrollIntoViewIfNeeded(el, false);
    measured = viewportRect(el);
  }

  const measuredLike = toRectLike(measured);
  if (intersectsViewport(measuredLike, viewport)) {
    const cx = measured.left + measured.width / 2;
    const cy = measured.top + measured.height / 2;
    const hit = hitTestPiercing(doc, el, cx, cy, viewport);
    if (hit === false) {
      return { interactable: false, reason: "obscured" };
    }
  }

  return { interactable: true, reason: "" };
}

function toRectLike(rect: { left: number; top: number; width: number; height: number }): RectLike {
  return {
    left: rect.left,
    top: rect.top,
    right: rect.left + rect.width,
    bottom: rect.top + rect.height,
    width: rect.width,
    height: rect.height,
  };
}

/** Build a fresh target payload for an already-resolved element. */
export function describeTargetElement(
  doc: Document,
  el: Element,
  id: number,
): ContentTargetPayload {
  const ownerDoc = el.ownerDocument ?? doc;
  const rect = viewportRect(el);
  const { interactable, reason } = describeInteractability(doc, el, rect);

  return {
    id,
    tag: el.tagName.toLowerCase(),
    type: typeToken(el),
    label: truncateText(labelFor(el, ownerDoc), LABEL_MAX_CHARS),
    x: Math.round(rect.left),
    y: Math.round(rect.top),
    w: Math.round(rect.width),
    h: Math.round(rect.height),
    interactable,
    reason,
    editable: isEditableElement(el),
    cssPath: cssPathFor(el),
  };
}

/** Look up `id` in the current snapshot map and describe it with fresh geometry. */
export function buildTargetPayload(
  doc: Document,
  index: Map<number, Element> | undefined,
  id: number,
): ContentTargetPayload {
  return describeTargetElement(doc, resolveIndexedElement(index, id), id);
}

export interface ElementValue {
  value: string;
  length: number;
  kind: "value" | "text";
  editable: boolean;
}

/** Current value of an indexed element: control value, contenteditable text, or textContent. */
export function readIndexedValue(
  doc: Document,
  index: Map<number, Element> | undefined,
  id: number,
): ElementValue {
  const el = resolveIndexedElement(index, id);
  const tag = el.tagName.toLowerCase();
  let value = "";
  let kind: "value" | "text" = "text";

  if (tag === "input" || tag === "textarea") {
    const raw = (el as HTMLInputElement | HTMLTextAreaElement).value;
    value = typeof raw === "string" ? raw : "";
    kind = "value";
  } else if (el.getAttribute("contenteditable") === "true") {
    value = (el as HTMLElement).innerText ?? el.textContent ?? "";
  } else {
    value = el.textContent ?? "";
  }

  if (value.length > READBACK_MAX_CHARS) {
    value = value.slice(0, READBACK_MAX_CHARS);
  }

  return {
    value,
    length: value.length,
    kind,
    editable: isEditableElement(el),
  };
}

/** Human-readable description of the topmost element at a viewport point. */
export function elementAtPoint(doc: Document, x: number, y: number): string {
  let currentDoc: Document = doc;
  let currentX = x;
  let currentY = y;
  let hit: Element | null = null;
  const seenFrames = new Set<Element>();

  for (let depth = 0; depth < 16; depth += 1) {
    const fn = currentDoc.elementFromPoint;
    if (typeof fn !== "function") {
      break;
    }
    let candidate: Element | null = null;
    try {
      candidate = fn.call(currentDoc, currentX, currentY);
    } catch {
      break;
    }
    if (!candidate) {
      break;
    }
    hit = candidate;

    while (
      hit &&
      (hit as Element).shadowRoot &&
      typeof (hit as Element).shadowRoot?.elementFromPoint === "function"
    ) {
      try {
        const inner = (hit as Element).shadowRoot!.elementFromPoint(currentX, currentY);
        if (!inner || inner === hit) {
          break;
        }
        hit = inner;
      } catch {
        break;
      }
    }

    const tag = hit.tagName.toLowerCase();
    if (tag === "iframe" || tag === "frame") {
      if (seenFrames.has(hit)) {
        break;
      }
      seenFrames.add(hit);
      let subDoc: Document | null = null;
      try {
        subDoc = (hit as HTMLIFrameElement).contentDocument ?? null;
      } catch {
        subDoc = null;
      }
      if (!subDoc) {
        break;
      }
      const frameRect = hit.getBoundingClientRect();
      const clientLeft = (hit as HTMLElement).clientLeft ?? 0;
      const clientTop = (hit as HTMLElement).clientTop ?? 0;
      currentX = currentX - frameRect.left - clientLeft;
      currentY = currentY - frameRect.top - clientTop;
      currentDoc = subDoc;
      continue;
    }

    break;
  }

  if (hit === null || hit === undefined) {
    return "";
  }
  const tag = hit.tagName.toLowerCase();
  const label = truncateText(labelFor(hit, hit.ownerDocument ?? currentDoc), LABEL_MAX_CHARS);
  return label === "" ? `<${tag}>` : `<${tag}> "${label}"`;
}

const ATTR_VALUE_MAX_CHARS = 1_000;

function absoluteURL(doc: Document, raw: string | null): string {
  if (raw === null || raw === "") {
    return "";
  }
  try {
    return new URL(raw, doc.baseURI).toString();
  } catch {
    return raw;
  }
}

/** Resolve one element by id or selector and return its identity and attributes. */
export function buildAttrPayload(
  doc: Document,
  index: Map<number, Element> | undefined,
  params: { id?: number; selector?: string; frame?: string },
): Omit<ContentAttrPayload, "docId" | "snapshotId"> {
  let targetDoc = doc;
  if (params.frame) {
    const resolved = resolveFramePath(doc, params.frame);
    if (!resolved) {
      throw new Error(`frame "${params.frame}" not found or cross-origin`);
    }
    targetDoc = resolved;
  }

  let el: Element;
  let matches = 1;

  if (typeof params.id === "number") {
    el = resolveIndexedElement(index, params.id);
  } else if (typeof params.selector === "string" && params.selector !== "") {
    let found: Element[];
    try {
      found = Array.from(targetDoc.querySelectorAll(params.selector));
    } catch (error) {
      throw new Error(`invalid selector "${params.selector}": ${errorText(error)}`);
    }
    if (found.length === 0) {
      throw new Error(`selector "${params.selector}" matched no elements`);
    }
    el = found[0];
    matches = found.length;
  } else {
    throw new Error("id or selector is required");
  }

  const rect = viewportRect(el);
  const tag = el.tagName.toLowerCase();
  let value = "";
  if (tag === "input" || tag === "textarea" || tag === "select") {
    value = String((el as HTMLInputElement).value ?? "");
  } else if (el.getAttribute("contenteditable") === "true") {
    value = (el as HTMLElement).innerText ?? "";
  }
  if (value.length > ATTR_VALUE_MAX_CHARS) {
    value = value.slice(0, ATTR_VALUE_MAX_CHARS);
  }

  const attributes: Record<string, string> = {};
  for (const attribute of Array.from(el.attributes)) {
    attributes[attribute.name] = attribute.value;
  }

  return {
    tag,
    role: roleFor(el),
    type: typeToken(el),
    label: truncateText(labelFor(el, el.ownerDocument ?? doc), LABEL_MAX_CHARS),
    value,
    state: stateTokens(el).join(", "),
    href: absoluteURL(doc, el.getAttribute("href")),
    src: absoluteURL(doc, el.getAttribute("src")),
    name: el.getAttribute("name") ?? "",
    elementId: el.getAttribute("id") ?? "",
    rect: {
      x: Math.round(rect.left),
      y: Math.round(rect.top),
      w: Math.round(rect.width),
      h: Math.round(rect.height),
    },
    attributes,
    matches,
  };
}

/** Scroll the element into view and move the caret to the end of its content. */
export function focusIndexedElement(
  doc: Document,
  index: Map<number, Element> | undefined,
  id: number,
): void {
  const el = resolveIndexedElement(index, id);
  scrollIntoViewIfNeeded(el, true);

  const focusable = el as HTMLElement & {
    focus?: () => void;
    value?: string;
    setSelectionRange?: (start: number, end: number) => void;
  };
  if (typeof focusable.focus === "function") {
    focusable.focus();
  }

  const tag = el.tagName.toLowerCase();
  if (tag === "input" || tag === "textarea") {
    const length = typeof focusable.value === "string" ? focusable.value.length : 0;
    if (typeof focusable.setSelectionRange === "function") {
      try {
        focusable.setSelectionRange(length, length);
      } catch {
        // Inputs such as type=email do not support selection; focus is enough.
      }
    }
    return;
  }

  if (el.getAttribute("contenteditable") === "true") {
    try {
      const range = doc.createRange();
      range.selectNodeContents(el);
      range.collapse(false);
      const selection = doc.defaultView?.getSelection() ?? null;
      if (selection) {
        selection.removeAllRanges();
        selection.addRange(range);
      }
    } catch {
      // Selection APIs are best-effort; typing still targets the focused node.
    }
  }
}

function windowLike(doc: Document): WindowLike {
  const view = doc.defaultView;
  return {
    innerHeight: view?.innerHeight ?? 0,
    innerWidth: view?.innerWidth ?? doc.documentElement?.clientWidth ?? 0,
    scrollX: view?.scrollX ?? 0,
    scrollY: view?.scrollY ?? 0,
  };
}

export function buildMetricsPayload(
  doc: Document,
  view?: WindowLike,
): ContentMetricsPayload {
  const windowInfo = view ?? windowLike(doc);
  const viewport = computeViewport(doc, windowInfo);
  const width =
    typeof windowInfo.innerWidth === "number" && windowInfo.innerWidth > 0
      ? windowInfo.innerWidth
      : (doc.documentElement?.clientWidth ?? 0);

  return {
    scrollY: viewport.scrollY,
    viewportHeight: viewport.viewportHeight,
    viewportWidth: width,
    totalHeight: viewport.totalHeight,
    percent: viewport.percent,
  };
}

function isDisabled(el: Element): boolean {
  const control = el as HTMLElement & { disabled?: boolean };
  return control.disabled === true || el.getAttribute("aria-disabled") === "true";
}

function isSubmitControl(el: Element): boolean {
  const tag = el.tagName.toLowerCase();
  if (tag === "button") {
    return true;
  }
  if (tag === "input") {
    return SUBMIT_INPUT_TYPES.has(effectiveInputType(el));
  }
  return false;
}

/** Snapshot of submit-like controls and their disabled state. */
export function buildStateEntries(
  doc: Document,
  index: Map<number, Element> | undefined,
): ContentStateEntry[] {
  const entries: ContentStateEntry[] = [];
  if (!index) {
    return entries;
  }

  for (const [id, el] of index) {
    if (!el.isConnected || !isSubmitControl(el)) {
      continue;
    }
    entries.push({
      id,
      tag: el.tagName.toLowerCase(),
      type: typeToken(el),
      label: truncateText(labelFor(el, el.ownerDocument ?? doc), LABEL_MAX_CHARS),
      disabled: isDisabled(el),
    });
  }
  return entries;
}

export interface WaitOptions {
  selector?: string;
  text?: string;
  ignoreCase?: boolean;
  idle?: boolean;
  timeoutMs?: number;
}

function clampTimeout(value: number | undefined): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return WAIT_DEFAULT_MS;
  }
  return Math.max(1, Math.min(WAIT_MAX_MS, Math.round(value)));
}

type TimeoutHandle = ReturnType<typeof setTimeout>;
type ObserverLike = { disconnect: () => void; observe: (target: Node, options: MutationObserverInit) => void };

function mutationObserverFor(doc: Document): (new (callback: MutationCallback) => ObserverLike) | null {
  const view = doc.defaultView as (Window & typeof globalThis) | null;
  const ctor = view?.MutationObserver ?? (globalThis as { MutationObserver?: typeof MutationObserver }).MutationObserver;
  return typeof ctor === "function" ? (ctor as unknown as new (cb: MutationCallback) => ObserverLike) : null;
}

function documentText(doc: Document): string {
  const body = doc.body;
  if (!body) {
    return "";
  }
  const inner = (body as HTMLElement).innerText;
  return typeof inner === "string" ? inner : (body.textContent ?? "");
}

/** Case-sensitive by default; `ignoreCase` folds both sides. */
export function textPresent(doc: Document, needle: string, ignoreCase = false): boolean {
  if (needle === "") {
    return true;
  }
  const haystack = documentText(doc);
  return ignoreCase
    ? haystack.toLowerCase().includes(needle.toLowerCase())
    : haystack.includes(needle);
}

/**
 * Wait for a selector to appear, for body text to appear, or for the page to go
 * idle. Never rejects on timeout; only an invalid selector is rejected.
 */
export function waitForContent(doc: Document, options: WaitOptions): Promise<ContentWaitPayload> {
  const timeoutMs = clampTimeout(options.timeoutMs);
  const selector = typeof options.selector === "string" ? options.selector : undefined;
  const text = typeof options.text === "string" && options.text !== "" ? options.text : undefined;
  const ignoreCase = options.ignoreCase === true;

  if (selector !== undefined) {
    try {
      doc.querySelector(selector);
    } catch (error) {
      return Promise.reject(
        new Error(`invalid selector "${selector}": ${errorText(error)}`),
      );
    }
  }

  return new Promise<ContentWaitPayload>((resolve) => {
    const started = Date.now();
    let finished = false;
    let timer: TimeoutHandle | undefined;
    let settle: TimeoutHandle | undefined;
    let poll: ReturnType<typeof setInterval> | undefined;
    let observer: ObserverLike | null = null;

    const cleanup = (): void => {
      if (timer !== undefined) {
        clearTimeout(timer);
        timer = undefined;
      }
      if (poll !== undefined) {
        clearInterval(poll);
        poll = undefined;
      }
      if (settle !== undefined) {
        clearTimeout(settle);
        settle = undefined;
      }
      if (observer !== null) {
        try {
          observer.disconnect();
        } catch {
          // Observer already gone.
        }
        observer = null;
      }
    };

    const finish = (matched: boolean): void => {
      if (finished) {
        return;
      }
      finished = true;
      cleanup();
      resolve({ waitedMs: Date.now() - started, matched });
    };

    timer = setTimeout(() => finish(false), timeoutMs);

    const observerCtor = mutationObserverFor(doc);
    const root: Node | null = doc.documentElement ?? doc.body;

    if (selector !== undefined) {
      const matches = (): boolean => {
        try {
          return doc.querySelector(selector) !== null;
        } catch {
          return false;
        }
      };

      if (matches()) {
        finish(true);
        return;
      }
      if (!observerCtor || root === null) {
        return;
      }
      observer = new observerCtor(() => {
        if (matches()) {
          finish(true);
        }
      });
      observer.observe(root, { childList: true, subtree: true, attributes: true });
      return;
    }

    if (text !== undefined) {
      const matches = (): boolean => textPresent(doc, text, ignoreCase);
      if (matches()) {
        finish(true);
        return;
      }
      // MutationObserver catches commits; the poll covers sites that swap
      // data into existing text nodes without a mutation the observer batches.
      const tick = (): void => {
        if (matches()) {
          finish(true);
        }
      };
      poll = setInterval(tick, 150);
      if (observerCtor && root !== null) {
        observer = new observerCtor(tick);
        observer.observe(root, {
          childList: true,
          subtree: true,
          characterData: true,
        });
      }
      return;
    }

    // Idle mode: 600 ms without mutations, once the document is fully loaded.
    const schedule = (delay: number): void => {
      if (settle !== undefined) {
        clearTimeout(settle);
      }
      settle = setTimeout(() => {
        settle = undefined;
        if (finished) {
          return;
        }
        if (doc.readyState === "complete") {
          finish(true);
        } else {
          schedule(100);
        }
      }, delay);
    };

    if (observerCtor && root !== null) {
      observer = new observerCtor(() => schedule(IDLE_SETTLE_MS));
      observer.observe(root, { childList: true, subtree: true, attributes: true });
    }
    schedule(IDLE_SETTLE_MS);
  });
}
