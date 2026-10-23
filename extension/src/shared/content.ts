import type {
  MacroTarget,
  PageResolveResult,
  SnapElement,
  ViewportInfo,
} from "./protocol.js";

export const CONTENT_CHANNEL = "chromeCtl";

export interface ContentPingRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "ping";
}

export interface ContentSnapRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "snap";
  all: boolean;
  formsOnly: boolean;
}

export interface ContentReadRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "read";
  id?: number;
  selector?: string;
}

export interface ContentTargetRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "target";
  id: number;
}

export interface ContentFocusRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "focus";
  id: number;
}

export interface ContentMetricsRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "metrics";
}

export interface ContentStateRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "state";
}

export interface ContentWaitRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "wait";
  selector?: string;
  idle?: boolean;
  timeoutMs?: number;
}

/** Build a resilient descriptor for an element already in the snapshot index. */
export interface ContentDescribeRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "describe";
  id: number;
}

/** Resolve a resilient descriptor, polling until `timeoutMs` elapses. */
export interface ContentResolveRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "resolve";
  target: MacroTarget;
  timeoutMs: number;
}

export type ContentRequest =
  | ContentPingRequest
  | ContentSnapRequest
  | ContentReadRequest
  | ContentTargetRequest
  | ContentFocusRequest
  | ContentMetricsRequest
  | ContentStateRequest
  | ContentWaitRequest
  | ContentDescribeRequest
  | ContentResolveRequest;

export interface SnapPayload {
  viewport: ViewportInfo;
  elements: SnapElement[];
  lines: string[];
}

/**
 * Fresh geometry for a snapshot id. `cssPath` addresses the element from the
 * page's main world so the debugger can resolve it for file uploads.
 */
export interface ContentTargetPayload {
  id: number;
  tag: string;
  type: string;
  label: string;
  x: number;
  y: number;
  w: number;
  h: number;
  interactable: boolean;
  reason: string;
  editable: boolean;
  cssPath: string;
}

export interface ContentStateEntry {
  id: number;
  tag: string;
  type: string;
  label: string;
  disabled: boolean;
}

export interface ContentMetricsPayload {
  scrollY: number;
  viewportHeight: number;
  viewportWidth: number;
  totalHeight: number;
  percent: number;
}

export interface ContentWaitPayload {
  waitedMs: number;
  matched: boolean;
}

export type ContentResponse =
  | { ok: true; type: "ping" }
  | { ok: true; type: "snap"; snap: SnapPayload }
  | { ok: true; type: "read"; markdown: string }
  | { ok: true; type: "target"; target: ContentTargetPayload }
  | { ok: true; type: "focus" }
  | { ok: true; type: "metrics"; metrics: ContentMetricsPayload }
  | { ok: true; type: "state"; entries: ContentStateEntry[] }
  | { ok: true; type: "wait"; wait: ContentWaitPayload }
  | { ok: true; type: "describe"; target: MacroTarget }
  | { ok: true; type: "resolve"; resolved: PageResolveResult }
  | { ok: false; error: string };

export const READ_MAX_CHARS = 100_000;
