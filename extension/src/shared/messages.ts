import type { RpcRequest, RpcResponse } from "./protocol.js";

export type ConnState = "unconfigured" | "connecting" | "connected" | "disconnected";

export type RuntimeMessage =
  | { target: "sw"; type: "ensureOffscreen" }
  | { target: "sw"; type: "getToken" }
  | { target: "sw"; type: "rpc"; request: RpcRequest }
  | { target: "offscreen"; type: "rpcResult"; response: RpcResponse }
  | { target: "offscreen"; type: "token.updated"; token?: string }
  | { target: "offscreen"; type: "status.get" }
  | { target: "offscreen"; type: "reconnect" };

export interface TokenReply {
  token?: string;
}

export interface WsStatusReply {
  state: ConnState;
  tokenConfigured: boolean;
  daemonVersion?: string;
  lastError?: string;
}
