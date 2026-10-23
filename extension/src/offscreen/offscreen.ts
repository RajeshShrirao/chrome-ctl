import {
  DAEMON_WS_URL,
  HEARTBEAT_MS,
  Methods,
  STALE_MS,
} from "../shared/protocol.js";
import type { RpcRequest, RpcResponse } from "../shared/protocol.js";
import type {
  ConnState,
  RuntimeMessage,
  TokenReply,
  WsStatusReply,
} from "../shared/messages.js";

const RPC_TIMEOUT_MS = 10_000;
const BACKOFF_BASE_MS = 1_000;
const BACKOFF_MAX_MS = 10_000;
const BACKOFF_FACTOR = 1.5;
const JITTER_RATIO = 0.2;

interface PendingCall {
  ws: WebSocket;
  timer: ReturnType<typeof setTimeout>;
}

let token: string | undefined;
let tokenConfigured = false;
let state: ConnState = "unconfigured";
let daemonVersion: string | undefined;
let lastError: string | undefined;

let socket: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let heartbeatTimer: ReturnType<typeof setInterval> | null = null;
let watchdogTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectAttempt = 0;

const pending = new Map<string, PendingCall>();

// Offscreen documents only expose chrome.runtime, so token storage stays in the
// service worker and is requested over the runtime message bus.
function requestToken(): Promise<void> {
  return new Promise((resolve) => {
    const message: RuntimeMessage = { target: "sw", type: "getToken" };
    try {
      chrome.runtime.sendMessage(message, (reply: TokenReply | undefined) => {
        if (chrome.runtime.lastError) {
          token = undefined;
          tokenConfigured = false;
          lastError = "could not reach service worker";
          resolve();
          return;
        }
        applyToken(
          typeof reply?.token === "string" && reply.token.length > 0 ? reply.token : undefined,
        );
        resolve();
      });
    } catch {
      token = undefined;
      tokenConfigured = false;
      lastError = "could not reach service worker";
      resolve();
    }
  });
}

function applyToken(next: string | undefined): void {
  token = next;
  tokenConfigured = next !== undefined;
  daemonVersion = undefined;

  if (next === undefined) {
    clearReconnectTimer();
    closeSocket();
    state = "unconfigured";
    lastError = undefined;
    return;
  }

  connect();
}

function sendJson(ws: WebSocket, payload: unknown): void {
  if (ws.readyState !== WebSocket.OPEN) {
    return;
  }
  try {
    ws.send(JSON.stringify(payload));
  } catch {
    // A failed send surfaces as a close event, which drives reconnection.
  }
}

function resolvePending(id: string, response: RpcResponse): void {
  const entry = pending.get(id);
  if (!entry) {
    return;
  }
  clearTimeout(entry.timer);
  pending.delete(id);
  sendJson(entry.ws, response);
}

function failPending(message: string): void {
  for (const id of [...pending.keys()]) {
    resolvePending(id, { jsonrpc: "2.0", id, error: { code: -32000, message } });
  }
}

function stopHeartbeat(): void {
  if (heartbeatTimer !== null) {
    clearInterval(heartbeatTimer);
    heartbeatTimer = null;
  }
}

function stopWatchdog(): void {
  if (watchdogTimer !== null) {
    clearTimeout(watchdogTimer);
    watchdogTimer = null;
  }
}

function clearReconnectTimer(): void {
  if (reconnectTimer !== null) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
}

function startHeartbeat(ws: WebSocket): void {
  stopHeartbeat();
  heartbeatTimer = setInterval(() => {
    if (socket !== ws) {
      return;
    }
    sendJson(ws, {
      jsonrpc: "2.0",
      method: Methods.systemPing,
      params: { timestamp: Math.floor(Date.now() / 1000) },
    });
  }, HEARTBEAT_MS);
}

function resetWatchdog(ws: WebSocket): void {
  stopWatchdog();
  watchdogTimer = setTimeout(() => {
    if (socket !== ws) {
      return;
    }
    lastError = "daemon stopped responding";
    ws.close();
  }, STALE_MS);
}

function closeSocket(): void {
  const ws = socket;
  socket = null;
  if (ws !== null) {
    ws.onopen = null;
    ws.onmessage = null;
    ws.onclose = null;
    ws.onerror = null;
    try {
      ws.close();
    } catch {
      // Already closing; nothing to clean up.
    }
  }
  stopHeartbeat();
  stopWatchdog();
  failPending("connection closed");
}

function scheduleReconnect(): void {
  if (token === undefined) {
    state = "unconfigured";
    return;
  }
  clearReconnectTimer();
  const base = Math.min(BACKOFF_BASE_MS * BACKOFF_FACTOR ** reconnectAttempt, BACKOFF_MAX_MS);
  const jitter = base * JITTER_RATIO * (Math.random() * 2 - 1);
  const delay = Math.max(200, Math.round(base + jitter));
  reconnectAttempt += 1;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, delay);
}

function handleOpen(ws: WebSocket): void {
  if (socket !== ws) {
    return;
  }
  state = "connected";
  lastError = undefined;
  reconnectAttempt = 0;
  startHeartbeat(ws);
  resetWatchdog(ws);
}

function handleClose(ws: WebSocket): void {
  if (socket !== ws) {
    return;
  }
  socket = null;
  stopHeartbeat();
  stopWatchdog();
  failPending("daemon connection closed");
  daemonVersion = undefined;

  if (token === undefined) {
    state = "unconfigured";
    return;
  }
  state = "disconnected";
  if (lastError === undefined) {
    lastError = "daemon connection closed";
  }
  scheduleReconnect();
}

function handleDaemonMessage(ws: WebSocket, raw: string): void {
  if (socket !== ws) {
    return;
  }
  resetWatchdog(ws);

  let frame: unknown;
  try {
    frame = JSON.parse(raw);
  } catch {
    return;
  }
  if (typeof frame !== "object" || frame === null) {
    return;
  }

  const message = frame as {
    id?: unknown;
    method?: unknown;
    params?: unknown;
  };

  // Reply frames (including pong with id null) carry no method and are ignored.
  if (typeof message.method !== "string") {
    return;
  }

  if (message.method === Methods.systemReady) {
    const params = message.params as { version?: unknown } | undefined;
    if (typeof params?.version === "string") {
      daemonVersion = params.version;
    }
    return;
  }

  if (typeof message.id !== "string") {
    return;
  }

  const request: RpcRequest = {
    jsonrpc: "2.0",
    id: message.id,
    method: message.method,
    params: message.params,
  };
  forwardRequest(ws, request);
}

function forwardRequest(ws: WebSocket, request: RpcRequest): void {
  const id = request.id;
  if (id === undefined) {
    return;
  }

  const timer = setTimeout(() => {
    resolvePending(id, {
      jsonrpc: "2.0",
      id,
      error: { code: -32603, message: "extension timed out" },
    });
  }, RPC_TIMEOUT_MS);
  pending.set(id, { ws, timer });

  const message: RuntimeMessage = { target: "sw", type: "rpc", request };
  try {
    chrome.runtime.sendMessage(message, (response: RpcResponse) => {
      const runtimeError = chrome.runtime.lastError;
      if (runtimeError) {
        resolvePending(id, {
          jsonrpc: "2.0",
          id,
          error: { code: -32603, message: "service worker unavailable" },
        });
        return;
      }
      if (response) {
        resolvePending(id, response);
      } else {
        resolvePending(id, {
          jsonrpc: "2.0",
          id,
          error: { code: -32603, message: "empty service worker response" },
        });
      }
    });
  } catch {
    resolvePending(id, {
      jsonrpc: "2.0",
      id,
      error: { code: -32603, message: "failed to reach service worker" },
    });
  }
}

function connect(): void {
  clearReconnectTimer();
  closeSocket();

  if (token === undefined) {
    state = "unconfigured";
    return;
  }

  daemonVersion = undefined;
  lastError = undefined;
  state = "connecting";

  let ws: WebSocket;
  try {
    ws = new WebSocket(`${DAEMON_WS_URL}?token=${encodeURIComponent(token)}`);
  } catch {
    lastError = "could not open connection";
    state = "disconnected";
    scheduleReconnect();
    return;
  }

  socket = ws;
  ws.onopen = () => handleOpen(ws);
  ws.onmessage = (event: MessageEvent) => {
    if (typeof event.data === "string") {
      handleDaemonMessage(ws, event.data);
    }
  };
  ws.onclose = () => handleClose(ws);
  ws.onerror = () => {
    if (socket !== ws) {
      return;
    }
    lastError = "connection error";
    handleClose(ws);
    try {
      ws.close();
    } catch {
      // Already closed by the browser.
    }
  };
}

async function reloadTokenAndConnect(): Promise<void> {
  await requestToken();
}

function currentState(): ConnState {
  if (!tokenConfigured) {
    return "unconfigured";
  }
  if (state === "connected" && socket?.readyState !== WebSocket.OPEN) {
    return "disconnected";
  }
  return state;
}

function statusReply(): WsStatusReply {
  const reply: WsStatusReply = {
    state: currentState(),
    tokenConfigured,
  };
  if (daemonVersion !== undefined) {
    reply.daemonVersion = daemonVersion;
  }
  if (lastError !== undefined) {
    reply.lastError = lastError;
  }
  return reply;
}

function isRuntimeMessage(value: unknown): value is RuntimeMessage {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as { target?: unknown; type?: unknown };
  return typeof candidate.target === "string" && typeof candidate.type === "string";
}

chrome.runtime.onMessage.addListener((message: unknown, _sender, sendResponse) => {
  if (!isRuntimeMessage(message) || message.target !== "offscreen") {
    return false;
  }

  switch (message.type) {
    case "status.get":
      sendResponse(statusReply());
      return false;
    case "rpcResult": {
      if (typeof message.response.id === "string") {
        resolvePending(message.response.id, message.response);
      }
      sendResponse({ ok: true });
      return false;
    }
    case "token.updated":
      applyToken(
        typeof message.token === "string" && message.token.length > 0 ? message.token : undefined,
      );
      sendResponse({ ok: true });
      return false;
    case "reconnect":
      void reloadTokenAndConnect().catch(() => {});
      sendResponse({ ok: true });
      return false;
    default:
      return false;
  }
});

async function start(): Promise<void> {
  await requestToken();
}

void start();
