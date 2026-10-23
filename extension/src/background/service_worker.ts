import { CONTENT_CHANNEL } from "../shared/content.js";
import type {
  ContentReadRequest,
  ContentRequest,
  ContentResponse,
} from "../shared/content.js";
import { ErrorCodes, Methods, TOKEN_KEY } from "../shared/protocol.js";
import type {
  ActionClickResult,
  ActionPressResult,
  ActionScrollResult,
  ActionTypeResult,
  ActionUploadResult,
  ExtensionStatusResult,
  MediaCaptureResult,
  PageReadResult,
  PageWaitResult,
  RpcRequest,
  RpcResponse,
  SnapElement,
  SnapResult,
  TabInfo,
  TabListResult,
  TabOpenResult,
  TabSwitchResult,
} from "../shared/protocol.js";
import type { RuntimeMessage, TokenReply } from "../shared/messages.js";
import {
  RpcFault,
  click,
  press,
  scroll,
  typeText,
  upload,
} from "./actions.js";
import { describeCaptureError, stripPngDataUrl } from "./capture.js";
import { handlePageDescribe, handlePageResolve } from "./resolver.js";
import type { ResolverDeps } from "./resolver.js";
import { sso } from "./sso.js";

const OFFSCREEN_PATH = "offscreen/offscreen.html";
const CONTENT_TIMEOUT_MS = 10_000;
const TAB_LOAD_TIMEOUT_MS = 8_000;
const ALLOWED_URL_PROTOCOLS = new Set(["http:", "https:", "file:"]);
const RESTRICTED_URL_PROTOCOLS = new Set([
  "chrome:",
  "chrome-extension:",
  "chrome-untrusted:",
  "edge:",
  "about:",
  "devtools:",
  "view-source:",
]);

async function hasOffscreenDocument(): Promise<boolean> {
  const contexts = await chrome.runtime.getContexts({
    contextTypes: [chrome.runtime.ContextType.OFFSCREEN_DOCUMENT],
    documentUrls: [chrome.runtime.getURL(OFFSCREEN_PATH)],
  });
  return contexts.length > 0;
}

async function ensureOffscreenDocument(): Promise<void> {
  if (await hasOffscreenDocument()) {
    return;
  }

  try {
    await chrome.offscreen.createDocument({
      url: OFFSCREEN_PATH,
      reasons: [chrome.offscreen.Reason.BLOBS],
      justification:
        "Maintain continuous authenticated WebSocket connection to local chrome-ctl daemon",
    });
  } catch (error) {
    // A concurrent caller may win the single-document race; treat that as success.
    if (await hasOffscreenDocument()) {
      return;
    }
    throw error;
  }
}

async function isDebuggerAttached(tabId: number | undefined): Promise<boolean> {
  if (tabId === undefined) {
    return false;
  }
  try {
    const targets = await chrome.debugger.getTargets();
    return targets.some((target) => target.tabId === tabId && target.attached);
  } catch {
    return false;
  }
}

async function buildStatus(): Promise<ExtensionStatusResult> {
  const [tabs, focusedTabs] = await Promise.all([
    chrome.tabs.query({}),
    chrome.tabs.query({ active: true, lastFocusedWindow: true }),
  ]);
  const active = focusedTabs[0];

  return {
    tabCount: tabs.length,
    activeTab: active
      ? {
          tabId: active.id ?? -1,
          title: active.title ?? "",
          url: active.url ?? "",
        }
      : null,
    debuggerAttached: await isDebuggerAttached(active?.id),
  };
}

async function readToken(): Promise<TokenReply> {
  try {
    const stored = await chrome.storage.local.get(TOKEN_KEY);
    const value = stored[TOKEN_KEY];
    return typeof value === "string" && value.length > 0 ? { token: value } : {};
  } catch {
    return {};
  }
}

function relayToken(token: string | undefined): void {
  const message: RuntimeMessage = { target: "offscreen", type: "token.updated", token };
  try {
    chrome.runtime.sendMessage(message, () => {
      // No offscreen document yet, or it is shutting down.
      void chrome.runtime.lastError;
    });
  } catch {
    // Offscreen document is not running.
  }
}

function errorResponse(
  id: string | null,
  code: number,
  message: string,
): RpcResponse {
  return { jsonrpc: "2.0", id, error: { code, message } };
}

function asParams(value: unknown): Record<string, unknown> {
  if (typeof value === "object" && value !== null && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return {};
}

function describeError(error: unknown): string {
  if (error instanceof Error && error.message !== "") {
    return error.message;
  }
  if (typeof error === "string" && error !== "") {
    return error;
  }
  return "unknown error";
}

function tabIdOf(tab: chrome.tabs.Tab): number {
  if (typeof tab.id !== "number") {
    throw new Error("tab is missing an id");
  }
  return tab.id;
}

/** Resolve an explicit tab id or the active tab of the last focused window. */
async function resolveTab(tabId?: number): Promise<chrome.tabs.Tab> {
  if (typeof tabId === "number") {
    try {
      return await chrome.tabs.get(tabId);
    } catch {
      throw new Error(`tab ${tabId} not found`);
    }
  }

  const [active] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  if (!active) {
    throw new Error("no active tab found");
  }
  return active;
}

function restrictedProtocol(url: string | undefined): string | null {
  if (!url) {
    return null;
  }
  try {
    const protocol = new URL(url).protocol;
    return RESTRICTED_URL_PROTOCOLS.has(protocol) ? protocol : null;
  } catch {
    return null;
  }
}

/** Promise wrapper around tabs.sendMessage with a hard timeout and lastError surfacing. */
function sendToContent(
  tabId: number,
  request: ContentRequest,
  timeoutMs: number = CONTENT_TIMEOUT_MS,
): Promise<ContentResponse> {
  return new Promise<ContentResponse>((resolve, reject) => {
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function finish(error: Error | null, response?: ContentResponse): void {
      if (settled) {
        return;
      }
      settled = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      if (error) {
        reject(error);
        return;
      }
      if (response === undefined) {
        reject(new Error("content script returned no response"));
        return;
      }
      resolve(response);
    }

    timer = setTimeout(() => {
      finish(new Error(`content script did not respond within ${timeoutMs / 1000}s`));
    }, timeoutMs);

    try {
      chrome.tabs.sendMessage(tabId, request, { frameId: 0 }, (response: ContentResponse | undefined) => {
        const lastError = chrome.runtime.lastError;
        if (lastError) {
          finish(new Error(lastError.message ?? "no receiver in tab"));
          return;
        }
        finish(null, response);
      });
    } catch (error) {
      finish(new Error(describeError(error)));
    }
  });
}

async function pingContent(tabId: number): Promise<boolean> {
  try {
    const response = await sendToContent(tabId, { channel: CONTENT_CHANNEL, type: "ping" });
    return response.ok === true && response.type === "ping";
  } catch {
    return false;
  }
}

/** Ensure the content indexer is alive in the tab's main frame (idempotent). */
async function ensureContentScriptInjected(tabId: number): Promise<void> {
  if (await pingContent(tabId)) {
    return;
  }

  let url: string | undefined;
  try {
    url = (await chrome.tabs.get(tabId)).url;
  } catch {
    url = undefined;
  }
  const protocol = restrictedProtocol(url);
  if (protocol) {
    throw new Error(`${protocol} pages do not allow content scripts`);
  }

  try {
    await chrome.scripting.executeScript({
      target: { tabId, frameIds: [0] },
      files: ["content/indexer.js"],
    });
  } catch (error) {
    // Another caller may have injected concurrently, or the zone is restricted.
    if (await pingContent(tabId)) {
      return;
    }
    throw new Error(`content script injection failed (${describeError(error)})`);
  }

  if (!(await pingContent(tabId))) {
    throw new Error("content script did not respond after injection");
  }
}

/** Wait for a tab load to complete, resolving false on timeout (never rejects). */
function waitForTabLoad(tabId: number, timeoutMs: number): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    function finish(loaded: boolean): void {
      if (settled) {
        return;
      }
      settled = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      chrome.tabs.onUpdated.removeListener(listener);
      resolve(loaded);
    }

    function listener(updatedTabId: number, changeInfo: chrome.tabs.OnUpdatedInfo): void {
      if (updatedTabId === tabId && changeInfo.status === "complete") {
        finish(true);
      }
    }

    timer = setTimeout(() => finish(false), timeoutMs);
    chrome.tabs.onUpdated.addListener(listener);

    void chrome.tabs
      .get(tabId)
      .then((tab) => {
        if (tab.status === "complete") {
          finish(true);
        }
      })
      .catch(() => {});
  });
}

function parseNavigationUrl(value: unknown): URL {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error("url is required");
  }

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    throw new Error(`invalid url: ${value}`);
  }

  if (!ALLOWED_URL_PROTOCOLS.has(parsed.protocol)) {
    throw new Error(
      `unsupported url scheme "${parsed.protocol}" (allowed: http:, https:, file:)`,
    );
  }
  return parsed;
}

async function handleTabList(params: Record<string, unknown>): Promise<TabListResult> {
  const windowId = typeof params.windowId === "number" ? params.windowId : undefined;
  const tabs = await chrome.tabs.query(
    windowId === undefined ? { lastFocusedWindow: true } : { windowId },
  );

  const infos: TabInfo[] = tabs.map((tab) => ({
    tabId: tab.id ?? -1,
    title: tab.title ?? "",
    url: tab.url ?? "",
    active: tab.active === true,
  }));
  return { tabs: infos };
}

async function handleTabOpen(params: Record<string, unknown>): Promise<TabOpenResult> {
  const url = parseNavigationUrl(params.url);

  let tab: chrome.tabs.Tab;
  if (params.newTab === true) {
    tab = await chrome.tabs.create({ url: url.href, active: true });
  } else {
    const current = await resolveTab();
    const updated = await chrome.tabs.update(tabIdOf(current), {
      url: url.href,
      active: true,
    });
    if (!updated) {
      throw new Error("failed to navigate the active tab");
    }
    tab = updated;
  }

  const tabId = tabIdOf(tab);
  const loaded = await waitForTabLoad(tabId, TAB_LOAD_TIMEOUT_MS);

  let finalTab = tab;
  try {
    finalTab = await chrome.tabs.get(tabId);
  } catch {
    // Keep the creation/update snapshot if the tab vanished.
  }

  return {
    tabId,
    url: finalTab.url ?? url.href,
    title: finalTab.title ?? "",
    loaded,
  };
}

async function handleTabSwitch(params: Record<string, unknown>): Promise<TabSwitchResult> {
  const tabId = params.tabId;
  if (typeof tabId !== "number") {
    throw new Error("tabId is required");
  }

  const tab = await chrome.tabs.update(tabId, { active: true });
  if (!tab) {
    throw new Error(`tab ${tabId} not found`);
  }

  if (typeof tab.windowId === "number") {
    try {
      await chrome.windows.update(tab.windowId, { focused: true });
    } catch {
      // Focusing the window is best-effort.
    }
  }

  return { success: true, activeTabId: tabId };
}

interface SnapRequestOptions {
  all?: boolean;
  formsOnly?: boolean;
}

/** Build a snapshot result (content injection included) for snap/--snap responses. */
async function buildSnapResult(
  tab: chrome.tabs.Tab,
  options: SnapRequestOptions = {},
): Promise<SnapResult> {
  const tabId = tabIdOf(tab);

  await ensureContentScriptInjected(tabId);
  const response = await sendToContent(tabId, {
    channel: CONTENT_CHANNEL,
    type: "snap",
    all: options.all === true,
    formsOnly: options.formsOnly === true,
  });

  if (!response.ok) {
    throw new Error(response.error);
  }
  if (response.type !== "snap") {
    throw new Error("unexpected content script response");
  }

  let current = tab;
  try {
    current = await chrome.tabs.get(tabId);
  } catch {
    // The tab vanished or navigated mid-snap; fall back to the resolved tab.
  }

  const { viewport, elements, lines } = response.snap;
  const formattedText = [
    `TAB [${tabId}] ${current.title ?? ""} | ${current.url ?? ""}`,
    `VIEWPORT: ${viewport.scrollY}-${viewport.scrollY + viewport.viewportHeight}px of ${viewport.totalHeight}px (${viewport.percent}%)`,
    ...lines,
  ].join("\n");

  return {
    tabId,
    title: current.title ?? "",
    url: current.url ?? "",
    viewport,
    elements,
    formattedText,
  };
}

async function handlePageSnap(params: Record<string, unknown>): Promise<SnapResult> {
  const requestedTabId = typeof params.tabId === "number" ? params.tabId : undefined;
  const tab = await resolveTab(requestedTabId);

  return buildSnapResult(tab, {
    all: params.all === true,
    formsOnly: params.formsOnly === true,
  });
}

async function handlePageRead(params: Record<string, unknown>): Promise<PageReadResult> {
  const requestedTabId = typeof params.tabId === "number" ? params.tabId : undefined;
  const tab = await resolveTab(requestedTabId);
  const tabId = tabIdOf(tab);

  await ensureContentScriptInjected(tabId);

  const request: ContentReadRequest = { channel: CONTENT_CHANNEL, type: "read" };
  if (typeof params.id === "number") {
    request.id = params.id;
  }
  if (typeof params.selector === "string") {
    request.selector = params.selector;
  }

  const response = await sendToContent(tabId, request);
  if (!response.ok) {
    throw new Error(response.error);
  }
  if (response.type !== "read") {
    throw new Error("unexpected content script response");
  }

  return { markdown: response.markdown };
}

/** Resolve the action tab, mapping lookup failures to TAB_NOT_FOUND. */
async function resolveActionTab(params: Record<string, unknown>): Promise<chrome.tabs.Tab> {
  const requestedTabId = typeof params.tabId === "number" ? params.tabId : undefined;
  try {
    return await resolveTab(requestedTabId);
  } catch (error) {
    throw new RpcFault(
      ErrorCodes.tabNotFound,
      requestedTabId === undefined
        ? describeError(error)
        : `tab ${requestedTabId} not found`,
    );
  }
}

function requireNumberParam(params: Record<string, unknown>, name: string): number {
  const value = params[name];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new RpcFault(ErrorCodes.internal, `${name} is required`);
  }
  return value;
}

function requireStringParam(params: Record<string, unknown>, name: string): string {
  const value = params[name];
  if (typeof value !== "string") {
    throw new RpcFault(ErrorCodes.internal, `${name} is required`);
  }
  return value;
}

async function handlePageWait(params: Record<string, unknown>): Promise<PageWaitResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);

  await ensureContentScriptInjected(tabId);

  const timeoutMs =
    typeof params.timeoutMs === "number" && Number.isFinite(params.timeoutMs)
      ? Math.max(1, Math.min(30_000, Math.round(params.timeoutMs)))
      : 10_000;

  const request: ContentRequest = {
    channel: CONTENT_CHANNEL,
    type: "wait",
    timeoutMs,
  };
  if (typeof params.selector === "string") {
    request.selector = params.selector;
  }
  if (params.idle === true) {
    request.idle = true;
  }

  const response = await sendToContent(
    tabId,
    request,
    Math.min(timeoutMs + 2_000, 32_000),
  );
  if (!response.ok) {
    throw new RpcFault(ErrorCodes.internal, response.error);
  }
  if (response.type !== "wait") {
    throw new RpcFault(ErrorCodes.internal, "unexpected content script response");
  }

  return response.wait;
}

async function handleActionClick(
  params: Record<string, unknown>,
): Promise<ActionClickResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const id = requireNumberParam(params, "id");

  await ensureContentScriptInjected(tabId);

  const result = await click(tabId, id, {
    doubleClick: params.doubleClick === true,
    sendContent: sendToContent,
  });

  if (params.snap === true) {
    result.snapshot = await buildSnapResult(tab);
  }
  return result;
}

async function handleActionType(
  params: Record<string, unknown>,
): Promise<ActionTypeResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const id = requireNumberParam(params, "id");
  const text = requireStringParam(params, "text");

  await ensureContentScriptInjected(tabId);

  const result = await typeText(tabId, id, text, {
    enter: params.enter === true,
    typoRate: typeof params.typoRate === "number" ? params.typoRate : undefined,
    sendContent: sendToContent,
  });

  if (params.snap === true) {
    result.snapshot = await buildSnapResult(tab);
  }
  return result;
}

async function handleActionPress(
  params: Record<string, unknown>,
): Promise<ActionPressResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const key = requireStringParam(params, "key");
  const modifiers = Array.isArray(params.modifiers)
    ? params.modifiers.filter((value): value is string => typeof value === "string")
    : [];

  return press(tabId, key, modifiers);
}

async function handleActionScroll(
  params: Record<string, unknown>,
): Promise<ActionScrollResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const amount = requireNumberParam(params, "amount");

  if (params.direction !== "down" && params.direction !== "up") {
    throw new RpcFault(ErrorCodes.internal, 'direction must be "down" or "up"');
  }

  await ensureContentScriptInjected(tabId);

  return scroll(tabId, {
    direction: params.direction,
    amount,
    sendContent: sendToContent,
  });
}

async function handleActionUpload(
  params: Record<string, unknown>,
): Promise<ActionUploadResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const id = requireNumberParam(params, "id");
  const filePath = requireStringParam(params, "filePath");

  await ensureContentScriptInjected(tabId);

  return upload(tabId, id, { filePath, sendContent: sendToContent });
}

/**
 * Capture the viewport as a PNG data URL plus CSS-pixel viewport metrics.
 * The daemon overlays Set-of-Mark badges, so nothing is drawn in the page.
 */
async function handleMediaCapture(
  params: Record<string, unknown>,
): Promise<MediaCaptureResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);

  // captureVisibleTab always grabs the active tab of a window.
  let activeTab = tab;
  if (tab.active !== true) {
    try {
      const updated = await chrome.tabs.update(tabId, { active: true });
      if (updated) {
        activeTab = updated;
      }
    } catch (error) {
      throw new RpcFault(
        ErrorCodes.internal,
        `could not activate tab ${tabId} for capture (${describeError(error)})`,
      );
    }
  }

  if (typeof activeTab.windowId === "number") {
    try {
      await chrome.windows.update(activeTab.windowId, { focused: true });
    } catch {
      // Focusing is best-effort; an active tab is enough for captureVisibleTab.
    }
  }

  await ensureContentScriptInjected(tabId);

  const metricsResponse = await sendToContent(tabId, {
    channel: CONTENT_CHANNEL,
    type: "metrics",
  });
  if (!metricsResponse.ok) {
    throw new RpcFault(ErrorCodes.internal, metricsResponse.error);
  }
  if (metricsResponse.type !== "metrics") {
    throw new RpcFault(ErrorCodes.internal, "unexpected content script response");
  }

  let elements: SnapElement[] | undefined;
  if (params.includeElements === true) {
    const snapResponse = await sendToContent(tabId, {
      channel: CONTENT_CHANNEL,
      type: "snap",
      all: false,
      formsOnly: false,
    });
    if (!snapResponse.ok) {
      throw new RpcFault(ErrorCodes.internal, snapResponse.error);
    }
    if (snapResponse.type !== "snap") {
      throw new RpcFault(ErrorCodes.internal, "unexpected content script response");
    }
    elements = snapResponse.snap.elements;
  }

  const windowId = activeTab.windowId;
  if (typeof windowId !== "number") {
    throw new RpcFault(ErrorCodes.internal, `tab ${tabId} is not attached to a window`);
  }

  let dataUrl: string;
  try {
    dataUrl = await chrome.tabs.captureVisibleTab(windowId, { format: "png" });
  } catch (error) {
    throw new RpcFault(ErrorCodes.internal, describeCaptureError(error));
  }

  const result: MediaCaptureResult = {
    tabId,
    pngBase64: stripPngDataUrl(dataUrl),
    viewportWidth: metricsResponse.metrics.viewportWidth,
    viewportHeight: metricsResponse.metrics.viewportHeight,
  };
  if (elements !== undefined) {
    result.elements = elements;
  }
  return result;
}

/** Shared plumbing for describe/resolve so the resolver stays import-cycle free. */
const resolverDeps: ResolverDeps = {
  resolveActionTab,
  ensureContentScriptInjected,
  sendToContent,
};

async function handleRpc(request: RpcRequest): Promise<RpcResponse> {
  const id = request.id ?? null;

  // Notifications (no id) have no response channel; never execute side effects.
  if (typeof request.id !== "string" || request.id === "") {
    return { jsonrpc: "2.0", id: null };
  }

  const params = asParams(request.params);
  try {
    switch (request.method) {
      case Methods.sessionStatus:
        return { jsonrpc: "2.0", id, result: await buildStatus() };
      case Methods.tabList:
        return { jsonrpc: "2.0", id, result: await handleTabList(params) };
      case Methods.tabOpen:
        return { jsonrpc: "2.0", id, result: await handleTabOpen(params) };
      case Methods.tabSwitch:
        return { jsonrpc: "2.0", id, result: await handleTabSwitch(params) };
      case Methods.pageSnap:
        return { jsonrpc: "2.0", id, result: await handlePageSnap(params) };
      case Methods.pageRead:
        return { jsonrpc: "2.0", id, result: await handlePageRead(params) };
      case Methods.pageWait:
        return { jsonrpc: "2.0", id, result: await handlePageWait(params) };
      case Methods.pageDescribe:
        return {
          jsonrpc: "2.0",
          id,
          result: await handlePageDescribe(params, resolverDeps),
        };
      case Methods.pageResolve:
        return {
          jsonrpc: "2.0",
          id,
          result: await handlePageResolve(params, resolverDeps),
        };
      case Methods.authSSO:
        return { jsonrpc: "2.0", id, result: await sso(params) };
      case Methods.actionClick:
        return { jsonrpc: "2.0", id, result: await handleActionClick(params) };
      case Methods.actionType:
        return { jsonrpc: "2.0", id, result: await handleActionType(params) };
      case Methods.actionPress:
        return { jsonrpc: "2.0", id, result: await handleActionPress(params) };
      case Methods.actionScroll:
        return { jsonrpc: "2.0", id, result: await handleActionScroll(params) };
      case Methods.actionUpload:
        return { jsonrpc: "2.0", id, result: await handleActionUpload(params) };
      case Methods.mediaCapture:
        return { jsonrpc: "2.0", id, result: await handleMediaCapture(params) };
      default:
        return errorResponse(id, -32601, "method not found");
    }
  } catch (error) {
    if (error instanceof RpcFault) {
      return errorResponse(id, error.code, error.message);
    }
    return errorResponse(id, -32603, describeError(error));
  }
}

function isRuntimeMessage(value: unknown): value is RuntimeMessage {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as { target?: unknown; type?: unknown };
  return typeof candidate.target === "string" && typeof candidate.type === "string";
}

chrome.runtime.onMessage.addListener((message: unknown, _sender, sendResponse) => {
  if (!isRuntimeMessage(message) || message.target !== "sw") {
    return false;
  }

  if (message.type === "ensureOffscreen") {
    ensureOffscreenDocument()
      .then(() => sendResponse({ ok: true }))
      .catch((error: unknown) =>
        sendResponse({ ok: false, error: error instanceof Error ? error.message : "failed" }),
      );
    return true;
  }

  if (message.type === "getToken") {
    void readToken().then(sendResponse);
    return true;
  }

  if (message.type === "rpc") {
    void handleRpc(message.request).then(sendResponse);
    return true;
  }

  return false;
});

chrome.storage.onChanged.addListener((changes, areaName) => {
  if (areaName !== "local" || !(TOKEN_KEY in changes)) {
    return;
  }
  const next = changes[TOKEN_KEY]?.newValue;
  relayToken(typeof next === "string" && next.length > 0 ? next : undefined);
});

chrome.runtime.onInstalled.addListener(() => {
  void ensureOffscreenDocument().catch(() => {});
});

chrome.runtime.onStartup.addListener(() => {
  void ensureOffscreenDocument().catch(() => {});
});

// Fire-and-forget on worker load; the popup and install hooks retry.
void ensureOffscreenDocument().catch(() => {});
