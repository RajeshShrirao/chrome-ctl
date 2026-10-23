import { CONTENT_CHANNEL, READ_MAX_CHARS } from "../shared/content.js";
import type { ContentRequest, ContentResponse, SnapPayload } from "../shared/content.js";
import type { MacroTarget } from "../shared/protocol.js";
import { buildDescriptor, resolveTarget } from "./describe.ts";
import { snapshot } from "./dom.js";
import {
  buildMetricsPayload,
  buildStateEntries,
  buildTargetPayload,
  focusIndexedElement,
  waitForContent,
} from "./interact.js";
import { extractMarkdown, resolveReadElement, truncateMarkdown } from "./read.js";

interface IndexerGlobal {
  /** Latest snapshot map, reused by later phases (click/type by element id). */
  __chromeCtlIndex?: Map<number, Element>;
  __chromeCtlIndexerInstalled?: boolean;
}

const scope = window as unknown as Window & IndexerGlobal;

const REQUEST_TYPES = new Set([
  "ping",
  "snap",
  "read",
  "target",
  "focus",
  "metrics",
  "state",
  "wait",
  "describe",
  "resolve",
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
  const result = snapshot(document, {
    all: request.all === true,
    formsOnly: request.formsOnly === true,
  });
  scope.__chromeCtlIndex = result.byId;
  return {
    viewport: result.viewport,
    elements: result.elements,
    lines: result.lines,
  };
}

function describeRead(request: ContentRequest & { type: "read" }): string {
  const target = resolveReadElement(
    document,
    scope.__chromeCtlIndex,
    request.id,
    request.selector,
  );
  return truncateMarkdown(extractMarkdown(document, target), READ_MAX_CHARS);
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
      return { ok: true, type: "read", markdown: describeRead(request) };
    case "target": {
      const target = buildTargetPayload(document, scope.__chromeCtlIndex, request.id);
      return { ok: true, type: "target", target };
    }
    case "focus":
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
    case "wait": {
      const wait = await waitForContent(document, {
        selector: request.selector,
        idle: request.idle,
        timeoutMs: request.timeoutMs,
      });
      return { ok: true, type: "wait", wait };
    }
    case "describe":
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
