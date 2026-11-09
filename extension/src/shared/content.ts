import type {
  FormControlState,
  MacroTarget,
  PageResolveResult,
  ReadLink,
  ReadTable,
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
  deep?: boolean;
  frames?: boolean;
  maxElements?: number;
}

export interface ContentReadRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "read";
  id?: number;
  selector?: string;
  mode?: "markdown" | "tables" | "links";
  maxTables?: number;
  maxLinks?: number;
  forms?: boolean;
  expectedDocId?: string;
}

export interface ContentTargetRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "target";
  id?: number;
  selector?: string;
  text?: string;
  expectedDocId?: string;
}

export interface ContentFocusRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "focus";
  id: number;
  expectedDocId?: string;
}

export interface ContentMetricsRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "metrics";
}

export interface ContentStateRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "state";
}

/** Read back an element's current value/text; used to verify typing. */
export interface ContentValueRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "value";
  id: number;
  expectedDocId?: string;
}

/** Topmost element at a viewport point; used to report where a click landed. */
export interface ContentPointRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "point";
  x: number;
  y: number;
}

/** Attributes and identity for one element, by snapshot id or CSS selector. */
export interface ContentAttrRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "attr";
  id?: number;
  selector?: string;
  frame?: string;
  expectedDocId?: string;
}

export interface ContentWaitRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "wait";
  selector?: string;
  text?: string;
  ignoreCase?: boolean;
  idle?: boolean;
  timeoutMs?: number;
}

/** Build a resilient descriptor for an element already in the snapshot index. */
export interface ContentDescribeRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "describe";
  id: number;
  expectedDocId?: string;
}

/** Resolve a resilient descriptor, polling until `timeoutMs` elapses. */
export interface ContentResolveRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "resolve";
  target: MacroTarget;
  timeoutMs: number;
}

/** Extract state of all form controls across same-origin frames and shadow roots. */
export interface ContentFormStateRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "formstate";
}

/** Check immediate consequence of a click on an element or page. */
export interface ContentConsequenceRequest {
  channel: typeof CONTENT_CHANNEL;
  type: "consequence";
  id?: number;
  initialUrl?: string;
  beforeChecked?: boolean;
  beforeValue?: string;
  beforeDisabled?: boolean;
}

export type ContentRequest =
  | ContentPingRequest
  | ContentSnapRequest
  | ContentReadRequest
  | ContentTargetRequest
  | ContentFocusRequest
  | ContentMetricsRequest
  | ContentStateRequest
  | ContentValueRequest
  | ContentPointRequest
  | ContentAttrRequest
  | ContentWaitRequest
  | ContentDescribeRequest
  | ContentResolveRequest
  | ContentFormStateRequest
  | ContentConsequenceRequest;

export interface SnapPayload {
  docId: string;
  snapshotId: number;
  omitted: number;
  viewport: ViewportInfo;
  elements: SnapElement[];
  lines: string[];
  framesSummary?: string;
}

/**
 * Fresh geometry for a snapshot id. `cssPath` addresses the element from the
 * page's main world so the debugger can resolve it for file uploads.
 */
export interface ContentTargetPayload {
  id: number;
  docId?: string;
  snapshotId?: number;
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

export interface ContentValuePayload {
  id: number;
  docId?: string;
  snapshotId?: number;
  value: string;
  length: number;
  kind: "value" | "text";
  editable: boolean;
}

export interface ContentPointPayload {
  landedOn: string;
}

export interface ContentAttrPayload {
  docId: string;
  snapshotId: number;
  tag: string;
  role: string;
  type: string;
  label: string;
  value: string;
  state: string;
  href: string;
  src: string;
  name: string;
  elementId: string;
  rect: { x: number; y: number; w: number; h: number };
  attributes: Record<string, string>;
  matches: number;
}

export type ContentResponse =
  | { ok: true; type: "ping" }
  | { ok: true; type: "snap"; snap: SnapPayload }
  | {
      ok: true;
      type: "read";
      markdown: string;
      tables?: ReadTable[];
      links?: ReadLink[];
      truncated?: boolean;
    }
  | { ok: true; type: "target"; target: ContentTargetPayload }
  | { ok: true; type: "focus" }
  | { ok: true; type: "metrics"; metrics: ContentMetricsPayload }
  | { ok: true; type: "state"; entries: ContentStateEntry[] }
  | { ok: true; type: "value"; value: ContentValuePayload }
  | { ok: true; type: "point"; point: ContentPointPayload }
  | { ok: true; type: "attr"; attr: ContentAttrPayload }
  | { ok: true; type: "wait"; wait: ContentWaitPayload }
  | { ok: true; type: "describe"; target: MacroTarget }
  | { ok: true; type: "resolve"; resolved: PageResolveResult }
  | { ok: true; type: "formstate"; controls: FormControlState[] }
  | { ok: true; type: "consequence"; consequence?: string }
  | { ok: false; error: string };

export const READ_MAX_CHARS = 100_000;
