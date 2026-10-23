/**
 * Phase 5 page.describe / page.resolve routing.
 *
 * The tab/content plumbing lives in the service worker; it is injected so this
 * module stays free of import cycles and easy to reason about.
 */

import { CONTENT_CHANNEL } from "../shared/content.js";
import type { ContentRequest, ContentResponse } from "../shared/content.js";
import { ErrorCodes } from "../shared/protocol.js";
import type {
  MacroTarget,
  PageDescribeResult,
  PageResolveResult,
} from "../shared/protocol.js";
import { RpcFault } from "./faults.js";

export interface ResolverDeps {
  resolveActionTab: (params: Record<string, unknown>) => Promise<chrome.tabs.Tab>;
  ensureContentScriptInjected: (tabId: number) => Promise<void>;
  sendToContent: (
    tabId: number,
    request: ContentRequest,
    timeoutMs?: number,
  ) => Promise<ContentResponse>;
}

const RESOLVE_DEFAULT_TIMEOUT_MS = 3_000;
const RESOLVE_MIN_TIMEOUT_MS = 250;
const RESOLVE_MAX_TIMEOUT_MS = 15_000;
/** Content polling deadline plus IPC slack before the hop itself times out. */
const RESOLVE_WAIT_MARGIN_MS = 2_000;

const TARGET_KEYS = [
  "testId",
  "ariaLabel",
  "role",
  "text",
  "tag",
  "type",
  "xpath",
  "cssSelector",
] as const;

const SELECTOR_KEYS = ["testId", "ariaLabel", "text", "xpath", "cssSelector"] as const;

function tabIdOf(tab: chrome.tabs.Tab): number {
  if (typeof tab.id !== "number") {
    throw new RpcFault(ErrorCodes.internal, "tab is missing an id");
  }
  return tab.id;
}

function clampResolveTimeout(value: unknown): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return RESOLVE_DEFAULT_TIMEOUT_MS;
  }
  return Math.max(
    RESOLVE_MIN_TIMEOUT_MS,
    Math.min(RESOLVE_MAX_TIMEOUT_MS, Math.round(value)),
  );
}

/** Accept only known string fields; at least one resolvable selector is required. */
export function normalizeTarget(raw: unknown): MacroTarget {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    throw new RpcFault(ErrorCodes.internal, "target must be an object");
  }

  const source = raw as Record<string, unknown>;
  const target: MacroTarget = {};
  for (const key of TARGET_KEYS) {
    const value = source[key];
    if (typeof value === "string" && value.trim() !== "") {
      target[key] = value;
    }
  }

  if (!SELECTOR_KEYS.some((key) => target[key] !== undefined)) {
    throw new RpcFault(ErrorCodes.internal, "target must include at least one selector");
  }
  return target;
}

export async function handlePageDescribe(
  params: Record<string, unknown>,
  deps: ResolverDeps,
): Promise<PageDescribeResult> {
  const id = params.id;
  if (typeof id !== "number" || !Number.isFinite(id)) {
    throw new RpcFault(ErrorCodes.internal, "id is required");
  }

  const tab = await deps.resolveActionTab(params);
  const tabId = tabIdOf(tab);
  await deps.ensureContentScriptInjected(tabId);

  const response = await deps.sendToContent(tabId, {
    channel: CONTENT_CHANNEL,
    type: "describe",
    id,
  });

  if (!response.ok) {
    throw new RpcFault(ErrorCodes.elementNotFound, response.error);
  }
  if (response.type !== "describe") {
    throw new RpcFault(ErrorCodes.internal, "unexpected content script response");
  }
  return { target: response.target };
}

export async function handlePageResolve(
  params: Record<string, unknown>,
  deps: ResolverDeps,
): Promise<PageResolveResult> {
  const target = normalizeTarget(params.target);
  const timeoutMs = clampResolveTimeout(params.timeoutMs);

  const tab = await deps.resolveActionTab(params);
  const tabId = tabIdOf(tab);
  await deps.ensureContentScriptInjected(tabId);

  const request: ContentRequest = {
    channel: CONTENT_CHANNEL,
    type: "resolve",
    target,
    timeoutMs,
  };
  const response = await deps.sendToContent(
    tabId,
    request,
    timeoutMs + RESOLVE_WAIT_MARGIN_MS,
  );

  if (!response.ok) {
    throw new RpcFault(ErrorCodes.elementNotFound, response.error);
  }
  if (response.type !== "resolve") {
    throw new RpcFault(ErrorCodes.internal, "unexpected content script response");
  }
  return response.resolved;
}
