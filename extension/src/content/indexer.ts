import { CONTENT_CHANNEL, READ_MAX_CHARS } from "../shared/content.ts";
import type { ContentRequest, ContentResponse, SnapPayload } from "../shared/content.ts";
import type { MacroTarget } from "../shared/protocol.js";
import { buildDescriptor, resolveTarget } from "./describe.ts";
import { snapshot } from "./dom.ts";
import { extractFormState } from "./forms.ts";
import {
  buildAttrPayload,
  buildMetricsPayload,
  buildStateEntries,
  buildTargetPayload,
  elementAtPoint,
  focusIndexedElement,
  readIndexedValue,
  waitForContent,
} from "./interact.ts";
import {
  extractLinks,
  extractMarkdown,
  extractTables,
  resolveReadElement,
  truncateMarkdown,
} from "./read.ts";

interface IndexerGlobal {
  /** Live snapshot map; ids are unique for the life of this document. */
  __chromeCtlIndex?: Map<number, Element>;
  __chromeCtlIndexerInstalled?: boolean;
  /** Next id to hand out; never decreases within a document. */
  __chromeCtlNextId?: number;
  /** Monotonic snapshot counter, reported so callers can spot a changed page. */
  __chromeCtlSnapshotId?: number;
  /** Identity of this content-script document; a navigation produces a new one. */
  __chromeCtlDocId?: string;
}

const scope = window as unknown as Window & IndexerGlobal;

/** UUID when available; the fallback still differs per document instance. */
function randomDocId(): string {
  const cryptoApi = globalThis.crypto as Crypto | undefined;
  if (cryptoApi && typeof cryptoApi.randomUUID === "function") {
    return cryptoApi.randomUUID();
  }
  return `doc-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function docState(): {
  docId: string;
  nextId: number;
  snapshotId: number;
} {
  if (typeof scope.__chromeCtlDocId !== "string" || scope.__chromeCtlDocId === "") {
    scope.__chromeCtlDocId = randomDocId();
  }
  if (typeof scope.__chromeCtlNextId !== "number") {
    scope.__chromeCtlNextId = 1;
  }
  if (typeof scope.__chromeCtlSnapshotId !== "number") {
    scope.__chromeCtlSnapshotId = 0;
  }
  return {
    docId: scope.__chromeCtlDocId,
    nextId: scope.__chromeCtlNextId,
    snapshotId: scope.__chromeCtlSnapshotId,
  };
}

/**
 * Reject ids minted by an earlier document (navigation/reload). The service
 * worker forwards the docId it observed at snap time.
 */
function assertDoc(expectedDocId: string | undefined): void {
  if (typeof expectedDocId !== "string" || expectedDocId === "") {
    return;
  }
  const state = docState();
  if (expectedDocId !== state.docId) {
    const when =
      state.snapshotId === 0
        ? "the last snapshot"
        : `snapshot ${state.snapshotId}`;
    throw new Error(`snapshot stale: page changed since ${when}; run snap`);
  }
}

const REQUEST_TYPES = new Set([
  "ping",
  "snap",
  "read",
  "target",
  "focus",
  "metrics",
  "state",
  "value",
  "point",
  "attr",
  "wait",
  "describe",
  "resolve",
  "formstate",
]);

function isContentRequest(value: unknown): value is ContentRequest {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as { channel?: unknown; type?: unknown };
  return (
    candidate.channel === CONTENT_CHANNEL &&
    typeof candidate.type === "string" &&
    REQUEST_TYPES.has(candidate.type)
  );
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}

function describeSnap(request: ContentRequest & { type: "snap" }): SnapPayload {
  const state = docState();
  const result = snapshot(document, {
    all: request.all === true,
    formsOnly: request.formsOnly === true,
    deep: request.deep === true,
    frames: request.frames === true,
    maxElements: request.maxElements,
    startId: state.nextId,
  });

  // Merge, never replace: ids stay valid until their element leaves the DOM.
  const index = indexMap();
  for (const [id, el] of index) {
    if (!el.isConnected) {
      index.delete(id);
    }
  }
  for (const [id, el] of result.byId) {
    index.set(id, el);
  }

  scope.__chromeCtlNextId = result.nextId;
  scope.__chromeCtlSnapshotId = state.snapshotId + 1;

  return {
    docId: state.docId,
    snapshotId: state.snapshotId + 1,
    omitted: result.omitted,
    viewport: result.viewport,
    elements: result.elements,
    lines: result.lines,
    framesSummary: result.framesSummary,
  };
}

function readTarget(request: ContentRequest & { type: "read" }): Element {
  if (typeof request.id === "number") {
    assertDoc(request.expectedDocId);
  }
  return resolveReadElement(
    document,
    scope.__chromeCtlIndex,
    request.id,
    request.selector,
  );
}

function describeRead(request: ContentRequest & { type: "read" }): ContentResponse {
  const target = readTarget(request);

  if (request.mode === "tables") {
    const extracted = extractTables(document, target, request.maxTables);
    return {
      ok: true,
      type: "read",
      markdown: "",
      tables: extracted.tables,
      truncated: extracted.truncated,
    };
  }
  if (request.mode === "links") {
    const extracted = extractLinks(document, target, request.maxLinks);
    return {
      ok: true,
      type: "read",
      markdown: "",
      links: extracted.links,
      truncated: extracted.truncated,
    };
  }

  return {
    ok: true,
    type: "read",
    markdown: truncateMarkdown(extractMarkdown(document, target), READ_MAX_CHARS),
  };
}

function indexMap(): Map<number, Element> {
  if (!scope.__chromeCtlIndex) {
    scope.__chromeCtlIndex = new Map<number, Element>();
  }
  return scope.__chromeCtlIndex;
}

function describeIndexedElement(id: number): MacroTarget {
  const element = scope.__chromeCtlIndex?.get(id);
  if (!element || !element.isConnected) {
    throw new Error(`element [${id}] not found in the current snapshot`);
  }
  return buildDescriptor(document, element);
}

async function handleRequest(request: ContentRequest): Promise<ContentResponse> {
  switch (request.type) {
    case "ping":
      return { ok: true, type: "ping" };
    case "snap":
      return { ok: true, type: "snap", snap: describeSnap(request) };
    case "read":
      return describeRead(request);
    case "target": {
      assertDoc(request.expectedDocId);
      const state = docState();
      const target = buildTargetPayload(document, scope.__chromeCtlIndex, request.id);
      return {
        ok: true,
        type: "target",
        target: { ...target, docId: state.docId, snapshotId: state.snapshotId },
      };
    }
    case "focus":
      assertDoc(request.expectedDocId);
      focusIndexedElement(document, scope.__chromeCtlIndex, request.id);
      return { ok: true, type: "focus" };
    case "metrics":
      return { ok: true, type: "metrics", metrics: buildMetricsPayload(document) };
    case "state":
      return {
        ok: true,
        type: "state",
        entries: buildStateEntries(document, scope.__chromeCtlIndex),
      };
    case "value": {
      assertDoc(request.expectedDocId);
      const state = docState();
      const value = readIndexedValue(document, scope.__chromeCtlIndex, request.id);
      return {
        ok: true,
        type: "value",
        value: {
          id: request.id,
          docId: state.docId,
          snapshotId: state.snapshotId,
          ...value,
        },
      };
    }
    case "point":
      return {
        ok: true,
        type: "point",
        point: { landedOn: elementAtPoint(document, request.x, request.y) },
      };
    case "attr": {
      if (typeof request.id === "number") {
        assertDoc(request.expectedDocId);
      }
      const state = docState();
      const attr = buildAttrPayload(document, scope.__chromeCtlIndex, {
        id: request.id,
        selector: request.selector,
        frame: request.frame,
      });
      return {
        ok: true,
        type: "attr",
        attr: { docId: state.docId, snapshotId: state.snapshotId, ...attr },
      };
    }
    case "wait": {
      const wait = await waitForContent(document, {
        selector: request.selector,
        text: request.text,
        ignoreCase: request.ignoreCase,
        idle: request.idle,
        timeoutMs: request.timeoutMs,
      });
      return { ok: true, type: "wait", wait };
    }
    case "describe":
      assertDoc(request.expectedDocId);
      return {
        ok: true,
        type: "describe",
        target: describeIndexedElement(request.id),
      };
    case "resolve": {
      const resolved = await resolveTarget(
        document,
        indexMap(),
        request.target,
        request.timeoutMs,
      );
      return { ok: true, type: "resolve", resolved };
    }
    case "formstate":
      return {
        ok: true,
        type: "formstate",
        controls: extractFormState(document),
      };
    default:
      return { ok: false, error: `unsupported request type` };
  }
}

function install(): void {
  chrome.runtime.onMessage.addListener((message: unknown, _sender, sendResponse) => {
    if (!isContentRequest(message)) {
      return false;
    }

    // Async: `wait` holds the response channel open until it settles.
    handleRequest(message).then(sendResponse, (error: unknown) => {
      sendResponse({ ok: false, error: errorMessage(error) } satisfies ContentResponse);
    });
    return true;
  });
}

// Idempotent: executeScript may run this bundle again in an already-indexed tab.
if (!scope.__chromeCtlIndexerInstalled) {
  scope.__chromeCtlIndexerInstalled = true;
  if (!scope.__chromeCtlIndex) {
    scope.__chromeCtlIndex = new Map<number, Element>();
  }
  install();
}
