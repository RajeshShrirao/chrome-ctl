import { CONTENT_CHANNEL } from "../shared/content.js";
import type {
  ContentFormStateRequest,
  ContentReadRequest,
  ContentRequest,
  ContentResponse,
  ContentSnapRequest,
} from "../shared/content.js";
import { ErrorCodes, Methods, TOKEN_KEY, TOKEN_PATTERN } from "../shared/protocol.js";
import type {
  ActionClickResult,
  ActionPressResult,
  ActionScrollResult,
  ActionTypeResult,
  ActionUploadResult,
  ExtensionStatusResult,
  EvalResult,
  FormControlState,
  MediaCaptureResult,
  PageFormStateResult,
  PageNetResult,
  PageReadResult,
  PageAttrResult,
  PageWaitResult,
  RpcRequest,
  RpcResponse,
  SnapElement,
  SnapResult,
  TabInfo,
  TabListResult,
  TabOpenResult,
  TabSwitchResult,
  WindowCloseResult,
  WindowInfo,
  WindowListResult,
  WindowResult,
  WindowUseResult,
} from "../shared/protocol.js";
import type { RuntimeMessage, TokenReply } from "../shared/messages.js";
import {
  RpcFault,
  click,
  clickAt,
  press,
  scroll,
  typeText,
  upload,
} from "./actions.js";
import { describeCaptureError, stripPngDataUrl } from "./capture.js";
import * as debuggerController from "./debugger.js";
import { forgetTab, isNetworkEnabled, noteNavigation, snapshot as netSnapshot, waitForQuiet } from "./netlog.js";
import { handlePageDescribe, handlePageResolve } from "./resolver.js";
import type { ResolverDeps } from "./resolver.js";
import { sso } from "./sso.js";
import {
  activeTabIn,
  automationActiveTab,
  lastFocusedActiveTab,
  lastFocusedWindowId,
  liveAutomationWindowId,
  readAutomationWindowId,
  writeAutomationWindowId,
} from "./windows.js";

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
  const tabs = await chrome.tabs.query({});
  // Report the tab actions would target: the automation window when one is
  // active, otherwise the user's focused tab.
  const active = (await automationActiveTab()) ?? (await lastFocusedActiveTab());

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

/** Resolve an explicit tab id, an explicit window, the automation window, or the focused tab. */
async function resolveTab(tabId?: number, windowId?: number): Promise<chrome.tabs.Tab> {
  if (typeof tabId === "number") {
    try {
      return await chrome.tabs.get(tabId);
    } catch {
      throw new Error(`tab ${tabId} not found`);
    }
  }

  if (typeof windowId === "number") {
    const tab = await activeTabIn(windowId);
    if (!tab) {
      throw new Error(`window ${windowId} has no active tab`);
    }
    return tab;
  }

  const automationTab = await automationActiveTab();
  if (automationTab) {
    return automationTab;
  }

  const active = await lastFocusedActiveTab();
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

/** Snapshot identity last emitted for a tab; replaced by the next snap. */
interface SnapIdentity {
  docId: string;
  snapshotId: number;
}

const lastSnap = new Map<number, SnapIdentity>();

chrome.tabs.onUpdated.addListener((tabId, changeInfo) => {
  if (changeInfo.status === "loading" || changeInfo.url !== undefined) {
    // Keep the identity on purpose: ids from the previous document must fail as
    // stale (`-32010`) until a new snap installs the current docId. Clearing it
    // here would let a renumbered id silently resolve against a new element.
    noteNavigation(tabId);
  }
});

chrome.tabs.onRemoved.addListener((tabId) => {
  lastSnap.delete(tabId);
  forgetTab(tabId);
});

function requestUsesSnapshotId(request: ContentRequest): boolean {
  switch (request.type) {
    case "target":
    case "focus":
    case "describe":
    case "value":
      return true;
    case "read":
      return typeof request.id === "number";
    case "attr":
      return typeof request.id === "number";
    default:
      return false;
  }
}

/**
 * Attach the document identity observed at snap time. The content script
 * rejects the request when the page has navigated since, instead of silently
 * resolving an old id against a different element.
 */
function sendWithIdentity(
  tabId: number,
  request: ContentRequest,
  timeoutMs?: number,
): Promise<ContentResponse> {
  const identity = lastSnap.get(tabId);
  if (identity === undefined || !requestUsesSnapshotId(request)) {
    return sendToContent(tabId, request, timeoutMs);
  }
  return sendToContent(
    tabId,
    { ...request, expectedDocId: identity.docId } as ContentRequest,
    timeoutMs,
  );
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
  const requestedWindowId =
    typeof params.windowId === "number" ? params.windowId : undefined;
  const windowId = requestedWindowId ?? (await liveAutomationWindowId());
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
  const requestedWindowId =
    typeof params.windowId === "number" ? params.windowId : undefined;

  let tab: chrome.tabs.Tab;
  if (params.newTab === true) {
    const targetWindow = requestedWindowId ?? (await liveAutomationWindowId());
    const createProperties: chrome.tabs.CreateProperties = {
      url: url.href,
      active: true,
    };
    if (targetWindow !== undefined) {
      createProperties.windowId = targetWindow;
    }
    tab = await chrome.tabs.create(createProperties);
  } else {
    const current = await resolveTab(undefined, requestedWindowId);
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

  // Activating a tab never focuses its window, so the user's current window keeps focus.
  // Implicit commands follow the tab into its window; otherwise they would keep
  // reading whichever window was tracked before.
  if (
    typeof tab.windowId === "number" &&
    tab.windowId !== (await liveAutomationWindowId())
  ) {
    await writeAutomationWindowId(tab.windowId);
  }
  return { success: true, activeTabId: tabId };
}

function windowSummary(
  window: chrome.windows.Window,
  automationId: number | undefined,
): WindowInfo | null {
  if (typeof window.id !== "number") {
    return null;
  }
  return {
    windowId: window.id,
    focused: window.focused === true,
    state: window.state ?? "normal",
    tabCount: window.tabs?.length ?? 0,
    automation: window.id === automationId,
  };
}

async function handleWindowList(): Promise<WindowListResult> {
  const automationId = await liveAutomationWindowId();
  const windows = await chrome.windows.getAll({ populate: true });
  return {
    windows: windows
      .map((window) => windowSummary(window, automationId))
      .filter((window): window is WindowInfo => window !== null),
  };
}

async function windowResult(windowId: number, created: boolean): Promise<WindowResult> {
  let tabCount = 0;
  let focused = false;
  try {
    const window = await chrome.windows.get(windowId, { populate: true });
    tabCount = window.tabs?.length ?? 0;
    focused = window.focused === true;
  } catch {
    // The window can vanish between creation and lookup; report what we know.
  }
  const active = await activeTabIn(windowId);
  return {
    windowId,
    activeTabId: typeof active?.id === "number" ? active.id : 0,
    tabCount,
    focused,
    created,
  };
}

async function handleWindowCreate(
  params: Record<string, unknown>,
): Promise<WindowResult> {
  const url =
    typeof params.url === "string" && params.url.trim() !== ""
      ? parseNavigationUrl(params.url).href
      : undefined;
  const focused = params.focused === true;

  const created = await chrome.windows.create(
    url === undefined
      ? { focused, type: "normal", state: "normal" }
      : { url, focused, type: "normal", state: "normal" },
  );
  const windowId = created?.id;
  if (typeof windowId !== "number") {
    throw new Error("Chrome did not return a window id");
  }

  try {
    const win = await chrome.windows.get(windowId);
    if (win && (win.state === "fullscreen" || win.state === "maximized")) {
      await chrome.windows.update(windowId, { state: "normal" });
    }
  } catch {
    // best-effort state reset
  }

  await writeAutomationWindowId(windowId);

  const active = await activeTabIn(windowId);
  if (active && url !== undefined && active.status !== "complete") {
    await waitForTabLoad(tabIdOf(active), TAB_LOAD_TIMEOUT_MS);
  }
  return windowResult(windowId, true);
}

async function handleWindowEnsure(): Promise<WindowResult> {
  const existing = await liveAutomationWindowId();
  if (existing !== undefined) {
    return windowResult(existing, false);
  }
  return handleWindowCreate({});
}

async function handleWindowUse(
  params: Record<string, unknown>,
): Promise<WindowUseResult> {
  const requestedWindowId =
    typeof params.windowId === "number" ? params.windowId : undefined;

  if (params.focused !== true && requestedWindowId === undefined) {
    await writeAutomationWindowId(undefined);
    return { cleared: true };
  }

  const windowId =
    params.focused === true ? await lastFocusedWindowId() : requestedWindowId;
  if (windowId === undefined) {
    throw new Error("no focused window found");
  }

  let window: chrome.windows.Window;
  try {
    window = await chrome.windows.get(windowId, { populate: true });
  } catch {
    throw new Error(`window ${windowId} not found`);
  }

  await writeAutomationWindowId(windowId);
  const active = await activeTabIn(windowId);
  return {
    windowId,
    activeTabId: typeof active?.id === "number" ? active.id : 0,
    tabCount: window.tabs?.length ?? 0,
    focused: window.focused === true,
  };
}

async function handleWindowClose(
  params: Record<string, unknown>,
): Promise<WindowCloseResult> {
  const windowId = params.windowId;
  if (typeof windowId !== "number") {
    throw new Error("windowId is required");
  }

  try {
    await chrome.windows.remove(windowId);
  } catch {
    throw new Error(`window ${windowId} not found`);
  }

  if ((await readAutomationWindowId()) === windowId) {
    await writeAutomationWindowId(undefined);
  }
  return { success: true, windowId };
}

interface SnapRequestOptions {
  all?: boolean;
  formsOnly?: boolean;
  deep?: boolean;
  frames?: boolean;
  diff?: boolean;
  maxElements?: number;
}

const lastSnapElements = new Map<number, SnapElement[]>();
const lastSnapUrl = new Map<number, string>();

const SNAP_SETTLE_MS = 250;
const URL_PROBE_MS = 25;

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Let a layout-changing action commit before snapshotting (best effort). */
async function settleBeforeSnap(tabId: number): Promise<void> {
  try {
    await sendToContent(
      tabId,
      { channel: CONTENT_CHANNEL, type: "wait", idle: true, timeoutMs: SNAP_SETTLE_MS },
      SNAP_SETTLE_MS + 2_000,
    );
  } catch {
    // The snapshot itself will surface any real content-script failure.
  }
}

/** Build a snapshot result (content injection included) for snap/--snap responses. */
async function buildSnapResult(
  tab: chrome.tabs.Tab,
  options: SnapRequestOptions = {},
): Promise<SnapResult> {
  const tabId = tabIdOf(tab);

  await ensureContentScriptInjected(tabId);
  const request: ContentSnapRequest = {
    channel: CONTENT_CHANNEL,
    type: "snap",
    all: options.all === true,
    formsOnly: options.formsOnly === true,
  };
  if (options.deep === true) {
    request.deep = true;
  }
  if (options.frames === true) {
    request.frames = true;
  }
  if (typeof options.maxElements === "number" && Number.isFinite(options.maxElements)) {
    request.maxElements = Math.max(1, Math.round(options.maxElements));
  }
  const response = await sendToContent(tabId, request);

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

  const { docId, snapshotId, omitted, viewport, elements, lines, framesSummary } = response.snap;
  const headerLines: string[] = [
    `SNAP ${snapshotId} | TAB [${tabId}] ${current.title ?? ""} | ${current.url ?? ""}`,
    `VIEWPORT: ${Math.round(viewport.scrollY)}-${Math.round(viewport.scrollY + viewport.viewportHeight)}px of ${Math.round(viewport.totalHeight)}px (${Math.round(viewport.percent)}%)`,
  ];
  if (framesSummary) {
    headerLines.push(framesSummary);
  }
  if (elements.length <= 5 && (framesSummary !== undefined || elements.length === 0)) {
    headerLines.push("NOTE: page content may be inside an iframe; try snap --frames");
  }

  let formattedText: string;
  const lastElements = lastSnapElements.get(tabId);
  lastSnapElements.set(tabId, elements);

  if (options.diff === true && lastElements && lastElements.length > 0 && current.url === lastSnapUrl.get(tabId)) {
    const oldById = new Map<number, SnapElement>();
    for (const el of lastElements) {
      oldById.set(el.id, el);
    }
    const added: string[] = [];
    const changed: string[] = [];

    for (const el of elements) {
      const old = oldById.get(el.id);
      if (!old) {
        added.push(`+ [${el.id}] <${el.tag}> "${el.label}" @${el.x},${el.y}`);
      } else if (old.label !== el.label || old.x !== el.x || old.y !== el.y) {
        changed.push(`~ [${el.id}] <${el.tag}> "${el.label}" @${el.x},${el.y}`);
      }
    }

    if (added.length > 0 || changed.length > 0) {
      const diffLines = [
        ...headerLines,
        `DIFF (+${added.length} added, ~${changed.length} changed):`,
        ...added,
        ...changed,
      ];
      formattedText = diffLines.join("\n");
    } else {
      formattedText = [
        ...headerLines,
        `DIFF: no interactive elements added or changed.`,
      ].join("\n");
    }
  } else {
    formattedText = [
      ...headerLines,
      ...lines,
      ...(omitted > 0
        ? [`OMITTED: ${omitted} candidates (use snap --deep / --max-elements to see more)`]
        : []),
    ].join("\n");
  }
  lastSnapUrl.set(tabId, current.url ?? "");

  lastSnap.set(tabId, { docId, snapshotId });

  return {
    tabId,
    title: current.title ?? "",
    url: current.url ?? "",
    docId,
    snapshotId,
    omitted,
    viewport,
    elements,
    formattedText,
    framesSummary,
  };
}

async function handlePageSnap(params: Record<string, unknown>): Promise<SnapResult> {
  const requestedTabId = typeof params.tabId === "number" ? params.tabId : undefined;
  const tab = await resolveTab(requestedTabId);

  const options: SnapRequestOptions = {
    all: params.all === true,
    formsOnly: params.formsOnly === true,
    deep: params.deep === true,
    frames: params.frames === true,
  };
  if (typeof params.maxElements === "number" && Number.isFinite(params.maxElements)) {
    options.maxElements = params.maxElements;
  }
  return buildSnapResult(tab, options);
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
  if (params.mode === "tables" || params.mode === "links") {
    request.mode = params.mode;
  }
  if (params.forms === true) {
    request.forms = true;
  }
  if (typeof params.maxTables === "number" && params.maxTables > 0) {
    request.maxTables = Math.round(params.maxTables);
  }
  if (typeof params.maxLinks === "number" && params.maxLinks > 0) {
    request.maxLinks = Math.round(params.maxLinks);
  }

  const response = await sendWithIdentity(tabId, request);
  if (!response.ok) {
    throw new Error(response.error);
  }
  if (response.type !== "read") {
    throw new Error("unexpected content script response");
  }

  return {
    markdown: response.markdown,
    tables: response.tables,
    links: response.links,
    truncated: response.truncated,
  };
}

async function handlePageAttr(params: Record<string, unknown>): Promise<PageAttrResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);

  const request: ContentRequest = { channel: CONTENT_CHANNEL, type: "attr" };
  if (typeof params.id === "number") {
    request.id = params.id;
  }
  if (typeof params.selector === "string" && params.selector !== "") {
    request.selector = params.selector;
  }
  if (typeof params.frame === "string" && params.frame !== "") {
    request.frame = params.frame;
  }
  if (request.id === undefined && request.selector === undefined) {
    throw new RpcFault(ErrorCodes.internal, "id or selector is required");
  }

  await ensureContentScriptInjected(tabId);
  const response = await sendWithIdentity(tabId, request);
  if (!response.ok) {
    throw new RpcFault(ErrorCodes.elementNotFound, response.error);
  }
  if (response.type !== "attr") {
    throw new RpcFault(ErrorCodes.internal, "unexpected content script response");
  }
  return response.attr;
}

async function handlePageFormState(params: Record<string, unknown>): Promise<PageFormStateResult> {
  const requestedTabId = typeof params.tabId === "number" ? params.tabId : undefined;
  const tab = await resolveTab(requestedTabId);
  const tabId = tabIdOf(tab);

  await ensureContentScriptInjected(tabId);

  const request: ContentFormStateRequest = { channel: CONTENT_CHANNEL, type: "formstate" };
  const response = await sendWithIdentity(tabId, request);
  if (!response.ok) {
    throw new Error(response.error);
  }
  if (response.type !== "formstate") {
    throw new Error("unexpected response type");
  }
  return { controls: response.controls };
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

function parseCoordinates(value: unknown): { x: number; y: number } | null {
  if (typeof value !== "object" || value === null) {
    return null;
  }
  const at = value as { x?: unknown; y?: unknown };
  if (
    typeof at.x !== "number" ||
    !Number.isFinite(at.x) ||
    typeof at.y !== "number" ||
    !Number.isFinite(at.y) ||
    at.x < 0 ||
    at.y < 0
  ) {
    throw new RpcFault(ErrorCodes.internal, "at must contain non-negative x and y in CSS pixels");
  }
  return { x: Math.round(at.x), y: Math.round(at.y) };
}

function clampWaitTimeout(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value)
    ? Math.max(1, Math.min(30_000, Math.round(value)))
    : 10_000;
}

async function contentWait(
  tabId: number,
  request: ContentRequest,
  timeoutMs: number,
): Promise<{ matched: boolean; waitedMs: number }> {
  await ensureContentScriptInjected(tabId);
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

interface WaitOutcome {
  matched: boolean;
  waitedMs: number;
  inflight?: number;
  requests?: number;
}

async function handlePageWait(params: Record<string, unknown>): Promise<PageWaitResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const timeoutMs = clampWaitTimeout(params.timeoutMs);
  const text =
    typeof params.text === "string" && params.text !== "" ? params.text : undefined;
  const network = params.network === true;
  const quietMs =
    typeof params.quietMs === "number" && Number.isFinite(params.quietMs)
      ? Math.max(50, Math.min(10_000, Math.round(params.quietMs)))
      : 500;
  const longPollMs =
    typeof params.longPollMs === "number" && Number.isFinite(params.longPollMs)
      ? Math.max(1_000, Math.round(params.longPollMs))
      : undefined;

  if (!network && text === undefined) {
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
    return contentWait(tabId, request, timeoutMs);
  }

  const waits: Promise<WaitOutcome>[] = [];

  if (network) {
    // Network events only exist while the debugger is attached, so this wait
    // opens the session itself instead of silently observing nothing.
    await debuggerController.ensureAttached(tabId);
    if (!isNetworkEnabled(tabId)) {
      throw new RpcFault(
        ErrorCodes.debuggerDetached,
        "network events unavailable on this tab: the debugger could not enable CDP Network (DevTools may be open)",
      );
    }
    waits.push(waitForQuiet(tabId, timeoutMs, quietMs, longPollMs));
  }

  if (text !== undefined) {
    const request: ContentRequest = {
      channel: CONTENT_CHANNEL,
      type: "wait",
      text,
      ignoreCase: params.ignoreCase === true,
      timeoutMs,
    };
    waits.push(contentWait(tabId, request, timeoutMs));
  }

  const outcomes = await Promise.all(waits);
  const matched = outcomes.every((outcome) => outcome.matched);
  const networkOutcome = network ? outcomes[0] : undefined;
  return {
    waitedMs: Math.max(...outcomes.map((outcome) => outcome.waitedMs)),
    matched,
    reason: matched ? undefined : "timeout",
    inflight: networkOutcome?.inflight,
    requests: networkOutcome?.requests,
  };
}

async function handlePageNet(params: Record<string, unknown>): Promise<PageNetResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const snap = await netSnapshot(tabId);

  const sinceMs =
    typeof params.sinceMs === "number" && params.sinceMs > 0
      ? Math.round(params.sinceMs)
      : 0;
  const limit =
    typeof params.limit === "number" && params.limit > 0
      ? Math.min(1000, Math.round(params.limit))
      : 100;
  const cutoff = sinceMs > 0 ? Date.now() - sinceMs : 0;

  let entries = snap.entries;
  if (cutoff > 0) {
    entries = entries.filter((entry) => (entry.startedAt ?? 0) >= cutoff);
  }
  if (params.errorsOnly === true) {
    entries = entries.filter(
      (entry) =>
        typeof entry.error === "string" ||
        (typeof entry.status === "number" && entry.status >= 400),
    );
  }
  if (entries.length > limit) {
    entries = entries.slice(-limit);
  }

  return {
    entries,
    enabledAt: snap.enabledAt > 0 ? snap.enabledAt : undefined,
    note:
      snap.enabledAt === 0
        ? "no network data yet: the log is collected only while the debugger is attached; run an action or wait --network first"
        : snap.note,
  };
}

async function handleActionClick(
  params: Record<string, unknown>,
): Promise<ActionClickResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const at = parseCoordinates(params.at);
  const hasId = typeof params.id === "number" && Number.isFinite(params.id) && params.id > 0;
  const id = hasId ? (params.id as number) : 0;
  const selector = typeof params.selector === "string" && params.selector.trim() !== "" ? params.selector.trim() : undefined;
  const text = typeof params.text === "string" && params.text.trim() !== "" ? params.text.trim() : undefined;

  if (at === null && !hasId && !selector && !text) {
    throw new RpcFault(ErrorCodes.internal, "id, selector, text, or at is required");
  }

  await ensureContentScriptInjected(tabId);

  const query = hasId ? id : { id: 0, selector, text };

  const result =
    at === null
      ? await click(tabId, query, {
          doubleClick: params.doubleClick === true,
          sendContent: sendWithIdentity,
        })
      : await clickAt(tabId, at.x, at.y, {
          doubleClick: params.doubleClick === true,
          sendContent: sendWithIdentity,
        });

  // Fast navigation check
  const initialURL = tab.url ?? "";
  if (initialURL !== "") {
    try {
      const current = await chrome.tabs.get(tabId);
      result.urlChanged = (current.url ?? "") !== initialURL;
      if (result.urlChanged && !result.consequence) {
        result.consequence = `navigated to ${current.url}`;
      }
    } catch {
      result.urlChanged = false;
    }
  }

  result.dispatchMs = result.durationMs;
  let settleMs = 0;
  if (params.snap === true) {
    const settleStart = Date.now();
    await settleBeforeSnap(tabId);
    settleMs = Date.now() - settleStart;
    result.snapshot = await buildSnapResult(tab, { diff: true });
  }
  result.settleMs = settleMs;
  result.durationMs = result.dispatchMs + result.settleMs;
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
    sendContent: sendWithIdentity,
  });

  if (params.snap === true) {
    await settleBeforeSnap(tabId);
    result.snapshot = await buildSnapResult(tab, { diff: true });
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

  await ensureContentScriptInjected(tabId);

  if (typeof params.toSelector === "string" && params.toSelector.trim() !== "") {
    return scroll(tabId, {
      toSelector: params.toSelector.trim(),
      sendContent: sendWithIdentity,
    });
  }

  const amount = requireNumberParam(params, "amount");

  if (params.direction !== "down" && params.direction !== "up") {
    throw new RpcFault(ErrorCodes.internal, 'direction must be "down" or "up"');
  }

  return scroll(tabId, {
    direction: params.direction,
    amount,
    sendContent: sendWithIdentity,
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

  return upload(tabId, id, { filePath, sendContent: sendWithIdentity });
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

  // captureVisibleTab only needs an active tab in its window. Do not focus the
  // window, so background capture never pulls focus away from the user.
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

  const CAPTURE_TIMEOUT_MS = 5_000;
  const MIN_PNG_BASE64_LENGTH = 200;

  let pngBase64: string;
  let capturePath: "captureVisibleTab" | "cdp" = "captureVisibleTab";
  const isFull = params.full === true;

  if (isFull) {
    try {
      const shot = await debuggerController.send<{ data?: string }>(
        tabId,
        "Page.captureScreenshot",
        { format: "png", captureBeyondViewport: true },
      );
      const raw = shot?.data;
      if (typeof raw !== "string" || raw.trim() === "") {
        throw new Error("CDP full-page capture returned an empty payload");
      }
      pngBase64 = stripPngDataUrl(
        raw.trim().startsWith("data:") ? raw : `data:image/png;base64,${raw.trim()}`,
      );
      capturePath = "cdp";
    } catch (fallbackError) {
      throw new RpcFault(
        ErrorCodes.internal,
        `full-page capture failed: ${describeError(fallbackError)}`,
      );
    }
  } else {
    try {
      // If window is minimized, captureVisibleTab will fail or produce a blank image.
      let windowMinimized = false;
      try {
        const win = await chrome.windows.get(windowId);
        if (win?.state === "minimized") {
          windowMinimized = true;
        }
      } catch {
        // ignore
      }
      if (windowMinimized) {
        throw new Error("window is minimized");
      }

      const capturePromise = chrome.tabs.captureVisibleTab(windowId, { format: "png" });
      const timeoutPromise = new Promise<never>((_, reject) =>
        setTimeout(
          () => reject(new Error(`captureVisibleTab timed out after ${CAPTURE_TIMEOUT_MS}ms`)),
          CAPTURE_TIMEOUT_MS,
        ),
      );
      const dataUrl = await Promise.race([capturePromise, timeoutPromise]);
      pngBase64 = stripPngDataUrl(dataUrl);
      if (pngBase64.length < MIN_PNG_BASE64_LENGTH) {
        throw new Error(
          `captureVisibleTab returned suspiciously small payload (${pngBase64.length} chars)`,
        );
      }
    } catch (visibleError) {
      // captureVisibleTab can fail for minimized windows, tiled surfaces, or
      // transient rate limits; the debugger session captures the same viewport.
      try {
        const shot = await debuggerController.send<{ data?: string }>(
          tabId,
          "Page.captureScreenshot",
          { format: "png", captureBeyondViewport: false },
        );
        const raw = shot?.data;
        if (typeof raw !== "string" || raw.trim() === "") {
          throw new Error("CDP capture returned an empty payload");
        }
        pngBase64 = stripPngDataUrl(
          raw.trim().startsWith("data:") ? raw : `data:image/png;base64,${raw.trim()}`,
        );
        capturePath = "cdp";
      } catch (fallbackError) {
        throw new RpcFault(
          ErrorCodes.internal,
          `capture failed — captureVisibleTab: ${describeCaptureError(visibleError)}; CDP fallback: ${describeError(fallbackError)}. ` +
            "Likely causes: the tab is not visible, DevTools or another debugger is attached, or the page is restricted.",
        );
      }
    }
  }

  const result: MediaCaptureResult = {
    tabId,
    pngBase64,
    capturePath,
    viewportWidth: metricsResponse.metrics.viewportWidth,
    viewportHeight: metricsResponse.metrics.viewportHeight,
  };
  if (elements !== undefined) {
    result.elements = elements;
  }
  return result;
}

const EVAL_DEFAULT_TIMEOUT_MS = 5_000;
const EVAL_MAX_TIMEOUT_MS = 30_000;
const EVAL_MAX_CHARS = 100_000;

interface CdpEvalResponse {
  result?: { type?: string; value?: unknown; description?: string };
  exceptionDetails?: { text?: string; exception?: { description?: string } };
}

async function evaluateOnce(
  tabId: number,
  expression: string,
  awaitPromise: boolean,
  timeoutMs: number,
): Promise<CdpEvalResponse> {
  const response = await debuggerController.send<CdpEvalResponse>(
    tabId,
    "Runtime.evaluate",
    {
      expression,
      returnByValue: true,
      awaitPromise,
      userGesture: true,
      timeout: timeoutMs,
    },
  );
  return response ?? {};
}

function capEval(result: EvalResult): EvalResult {
  if (result.json.length <= EVAL_MAX_CHARS) {
    return result;
  }
  return { ...result, json: result.json.slice(0, EVAL_MAX_CHARS), truncated: true };
}

/** page.eval: CDP Runtime.evaluate, which bypasses page CSP for the expression. */
async function handlePageEval(params: Record<string, unknown>): Promise<EvalResult> {
  const tab = await resolveActionTab(params);
  const tabId = tabIdOf(tab);
  const expression = requireStringParam(params, "expression");
  if (expression.trim() === "") {
    throw new RpcFault(ErrorCodes.internal, "expression is required");
  }

  const awaitPromise = params.awaitPromise === true;
  let timeoutMs = EVAL_DEFAULT_TIMEOUT_MS;
  if (typeof params.timeoutMs === "number" && Number.isFinite(params.timeoutMs)) {
    timeoutMs = Math.max(250, Math.min(EVAL_MAX_TIMEOUT_MS, Math.round(params.timeoutMs)));
  }

  const audit = expression.replace(/\s+/g, " ").slice(0, 120);
  console.info(`[chrome-ctl] eval tab=${tabId} await=${awaitPromise} expr=${audit}`);

  let response: CdpEvalResponse;
  try {
    response = await evaluateOnce(tabId, expression, awaitPromise, timeoutMs);
  } catch (error) {
    const text = describeError(error);
    if (!/return.*value|serializ/i.test(text)) {
      throw new RpcFault(ErrorCodes.internal, `eval failed: ${text}`);
    }
    // Non-serialisable values (DOM nodes, maps, typed arrays) need explicit JSON.
    const wrapped = `JSON.stringify((() => { const __v = (${expression}); return __v === undefined ? null : __v; })())`;
    const fallback = await evaluateOnce(tabId, wrapped, awaitPromise, timeoutMs);
    if (fallback.exceptionDetails) {
      throw new RpcFault(ErrorCodes.internal, `eval threw: ${evalExceptionText(fallback)}`);
    }
    const json = fallback.result?.value;
    if (typeof json !== "string") {
      throw new RpcFault(ErrorCodes.internal, `eval result is not JSON-serialisable: ${text}`);
    }
    return capEval({ json, type: "string" });
  }

  if (response.exceptionDetails) {
    throw new RpcFault(ErrorCodes.internal, `eval threw: ${evalExceptionText(response)}`);
  }

  const outcome = response.result ?? {};
  let json: string;
  if (outcome.type === "undefined") {
    json = "null";
  } else if (outcome.value !== undefined) {
    try {
      json = JSON.stringify(outcome.value) ?? "null";
    } catch {
      json = JSON.stringify(outcome.description ?? String(outcome.value));
    }
  } else {
    json = JSON.stringify(outcome.description ?? null);
  }

  return capEval({ json, type: outcome.type });
}

function evalExceptionText(response: CdpEvalResponse): string {
  return (
    response.exceptionDetails?.exception?.description ??
    response.exceptionDetails?.text ??
    "uncaught exception"
  );
}

/** Shared plumbing for describe/resolve so the resolver stays import-cycle free. */const resolverDeps: ResolverDeps = {
  resolveActionTab,
  ensureContentScriptInjected,
  sendToContent: sendWithIdentity,
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
      case Methods.windowList:
        return { jsonrpc: "2.0", id, result: await handleWindowList() };
      case Methods.windowCreate:
        return { jsonrpc: "2.0", id, result: await handleWindowCreate(params) };
      case Methods.windowEnsure:
        return { jsonrpc: "2.0", id, result: await handleWindowEnsure() };
      case Methods.windowUse:
        return { jsonrpc: "2.0", id, result: await handleWindowUse(params) };
      case Methods.windowClose:
        return { jsonrpc: "2.0", id, result: await handleWindowClose(params) };
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
      case Methods.pageEval:
        return { jsonrpc: "2.0", id, result: await handlePageEval(params) };
      case Methods.pageAttr:
        return { jsonrpc: "2.0", id, result: await handlePageAttr(params) };
      case Methods.pageFormState:
        return { jsonrpc: "2.0", id, result: await handlePageFormState(params) };
      case Methods.pageNet:
        return { jsonrpc: "2.0", id, result: await handlePageNet(params) };
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

  if (message.type === "pairToken") {
    if (!TOKEN_PATTERN.test(message.token)) {
      sendResponse({ ok: false });
      return false;
    }
    void chrome.storage.local
      .set({ [TOKEN_KEY]: message.token })
      .then(() => sendResponse({ ok: true }))
      .catch(() => sendResponse({ ok: false }));
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
