import type { RpcRequest, RpcResponse } from "./protocol.js";

export type ConnState = "unconfigured" | "connecting" | "connected" | "disconnected";

export type RuntimeMessage =
  | { target: "sw"; type: "ensureOffscreen" }
  | { target: "sw"; type: "getToken" }
  | { target: "sw"; type: "pairToken"; token: string }
  | { target: "sw"; type: "rpc"; request: RpcRequest }
  | { target: "offscreen"; type: "rpcResult"; response: RpcResponse }
  | { target: "offscreen"; type: "token.updated"; token?: string }
  | { target: "offscreen"; type: "status.get" }
  | { target: "offscreen"; type: "pair" }
  | { target: "offscreen"; type: "reconnect" };

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
