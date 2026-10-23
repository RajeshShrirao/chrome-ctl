export const DAEMON_WS_URL = "ws://127.0.0.1:9229/ws";
export const HEARTBEAT_MS = 15_000;
export const STALE_MS = 45_000;
export const TOKEN_KEY = "chromeCtlToken";

export const Methods = {
  systemReady: "system.ready",
  systemPing: "system.ping",
  sessionStatus: "session.status",
} as const;

export interface RpcRequest {
  jsonrpc: "2.0";
  id?: string;
  method: string;
  params?: unknown;
}

export interface RpcResponse {
  jsonrpc: "2.0";
  id: string | null;
  result?: unknown;
  error?: { code: number; message: string; data?: unknown };
}

export interface ActiveTab {
  tabId: number;
  title: string;
  url: string;
}

export interface ExtensionStatusResult {
  activeTab?: ActiveTab | null;
  tabCount: number;
  debuggerAttached: boolean;
}

export interface SystemReadyParams {
  version: string;
  daemonUptimeSec: number;
  attachedTabs: number[];
}
