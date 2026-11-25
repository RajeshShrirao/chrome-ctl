import type { RpcRequest, RpcResponse } from "./protocol.js";

export type ConnState = "unconfigured" | "connecting" | "connected" | "disconnected";

export type ActivityStatus = "running" | "ok" | "error";

export interface ActivityEntry {
  id: string;
  method: string;
  summary: string;
  tabId?: number;
  startedAt: number;
  durationMs?: number;
  status: ActivityStatus;
  error?: string;
}

export interface UiStateSnapshot {
  connState: ConnState;
  paused: boolean;
  inflightCount: number;
  hasError: boolean;
  lastError?: string;
  automationWindowId?: number;
  debuggerAttached: boolean;
  activities: ActivityEntry[];
}

export type RuntimeMessage =
  | { target: "sw"; type: "ensureOffscreen" }
  | { target: "sw"; type: "getToken" }
  | { target: "sw"; type: "pairToken"; token: string }
  | { target: "sw"; type: "rpc"; request: RpcRequest }
  | { target: "sw"; type: "conn.state"; state: ConnState; lastError?: string }
  | { target: "sw"; type: "ui.getState" }
  | { target: "sw"; type: "ui.clearFeed" }
  | { target: "sw"; type: "ui.clearError" }
  | { target: "sw"; type: "ui.focusAutomation" }
  | { target: "sw"; type: "ui.detachDebugger" }
  | { target: "sw"; type: "ui.setPaused"; paused: boolean }
  | { target: "offscreen"; type: "rpcResult"; response: RpcResponse }
  | { target: "offscreen"; type: "token.updated"; token?: string }
  | { target: "offscreen"; type: "status.get" }
  | { target: "offscreen"; type: "pair" }
  | { target: "offscreen"; type: "reconnect" }
  | { target: "offscreen"; type: "daemon.control"; method: "session.pause" | "session.resume" }
  | { target: "ui"; type: "ui.state"; snapshot: UiStateSnapshot };

export interface TokenReply {
  token?: string;
}

export interface WsStatusReply {
  state: ConnState;
  tokenConfigured: boolean;
  pairing?: boolean;
  daemonVersion?: string;
  daemonProtocolVersion?: string;
  daemonCapabilities?: string[];
  lastError?: string;
}
