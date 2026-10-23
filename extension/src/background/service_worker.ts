import { Methods, TOKEN_KEY } from "../shared/protocol.js";
import type { ExtensionStatusResult, RpcRequest, RpcResponse } from "../shared/protocol.js";
import type { RuntimeMessage, TokenReply } from "../shared/messages.js";

const OFFSCREEN_PATH = "offscreen/offscreen.html";

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

async function handleRpc(request: RpcRequest): Promise<RpcResponse> {
  const id = request.id ?? null;
  try {
    if (request.method === Methods.sessionStatus) {
      return { jsonrpc: "2.0", id, result: await buildStatus() };
    }
    return errorResponse(id, -32601, "method not found");
  } catch {
    return errorResponse(id, -32603, "internal error");
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
