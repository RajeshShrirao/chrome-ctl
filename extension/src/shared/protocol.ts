export const DAEMON_WS_URL = "ws://127.0.0.1:9229/ws";
export const DAEMON_PAIR_URL = "http://127.0.0.1:9229/pair";
export const PAIR_HEADER = "X-Chrome-Ctl-Pair";
export const HEARTBEAT_MS = 15_000;
export const STALE_MS = 45_000;
export const TOKEN_KEY = "chromeCtlToken";
export const TOKEN_PATTERN = /^[0-9a-f]{64}$/;

export {
  CAPABILITIES,
  DEFAULT_RPC_TIMEOUT_MS,
  ErrorCodes,
  Methods,
  PROTOCOL_VERSION,
  rpcTimeoutMs,
} from "./protocol.generated.ts";

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
  protocolVersion?: string;
  capabilities?: string[];
}

export interface SystemReadyParams {
  version: string;
  protocolVersion?: string;
  capabilities?: string[];
  daemonUptimeSec: number;
  attachedTabs: number[];
}

export interface TabInfo {
  tabId: number;
  title: string;
  url: string;
  active: boolean;
}

export interface TabListResult {
  tabs: TabInfo[];
}

export interface TabOpenResult {
  tabId: number;
  url: string;
  title: string;
  loaded: boolean;
}

export interface TabSwitchResult {
  success: boolean;
  activeTabId: number;
}

export interface WindowInfo {
  windowId: number;
  focused: boolean;
  state: string;
  tabCount: number;
  automation: boolean;
}

export interface WindowListResult {
  windows: WindowInfo[];
}

export interface WindowResult {
  windowId: number;
  activeTabId: number;
  tabCount: number;
  focused: boolean;
  created: boolean;
}

export interface WindowUseResult {
  windowId?: number;
  activeTabId?: number;
  tabCount?: number;
  focused?: boolean;
  cleared?: boolean;
}

export interface WindowCloseResult {
  success: boolean;
  windowId: number;
}

export interface ViewportInfo {
  scrollY: number;
  viewportHeight: number;
  totalHeight: number;
  percent: number;
}

export type SnapChange = "added" | "same" | "updated" | "moved";

export interface SnapElement {
  id: number;
  tag: string;
  type: string;
  label: string;
  value: string;
  state: string;
  x: number;
  y: number;
  w: number;
  h: number;
  frame?: string;
  /** Change since the previous snapshot in this document. */
  change?: SnapChange;
}

export interface SnapResult {
  tabId: number;
  title: string;
  url: string;
  docId: string;
  snapshotId: number;
  omitted: number;
  viewport: ViewportInfo;
  elements: SnapElement[];
  formattedText: string;
  framesSummary?: string;
  /** Ids that left the document since the previous snapshot. */
  removed?: number[];
}

export interface ReadTable {
  index: number;
  caption?: string;
  headers: string[];
  rows: string[][];
}

export interface ReadLink {
  text: string;
  href: string;
}

export interface PageReadResult {
  markdown?: string;
  tables?: ReadTable[];
  links?: ReadLink[];
  truncated?: boolean;
}

export interface ActionTarget {
  id: number;
  tag: string;
  label: string;
}

export interface ActionCoordinates {
  x: number;
  y: number;
}

export interface ActionClickParams {
  tabId?: number;
  id?: number;
  selector?: string;
  text?: string;
  at?: ActionCoordinates;
  doubleClick?: boolean;
  snap?: boolean;
  timeoutMs?: number;
  seed?: string;
}

export interface ActionClickResult {
  success: boolean;
  action: string;
  target?: ActionTarget;
  coordinates?: ActionCoordinates;
  landedOn?: string;
  urlChanged?: boolean;
  consequence?: string;
  seed?: string;
  durationMs: number;
  dispatchMs?: number;
  settleMs?: number;
  snapshot?: SnapResult;
}

export interface ActionTypeParams {
  tabId?: number;
  id: number;
  text: string;
  enter?: boolean;
  typoRate?: number;
  snap?: boolean;
  seed?: string;
}

export interface ActionTypeResult {
  success: boolean;
  charsTyped: number;
  durationMs: number;
  typosInjected: number;
  stateDelta?: string;
  verified: boolean;
  verification?: string;
  readBack?: string;
  seed?: string;
  snapshot?: SnapResult;
}

export interface ActionPressParams {
  tabId?: number;
  key: string;
  modifiers?: string[];
  seed?: string;
}

export interface ActionPressResult {
  success: boolean;
  key: string;
  seed?: string;
}

export interface ActionScrollParams {
  tabId?: number;
  direction?: "down" | "up";
  amount?: number;
  toSelector?: string;
  seed?: string;
}

export interface ActionScrollResult {
  scrolledPixels: number;
  newScrollY: number;
  viewportPercent: number;
  target?: string;
  frame?: string;
  seed?: string;
}

export interface ActionUploadParams {
  tabId?: number;
  id: number;
  filePath: string;
}

export interface ActionUploadResult {
  success: boolean;
  fileName: string;
  fileSize: number;
}

export interface PageWaitParams {
  tabId?: number;
  selector?: string;
  idle?: boolean;
  text?: string;
  ignoreCase?: boolean;
  network?: boolean;
  quietMs?: number;
  longPollMs?: number;
  timeoutMs?: number;
}

export interface PageWaitResult {
  waitedMs: number;
  matched: boolean;
  reason?: string;
  inflight?: number;
  requests?: number;
}

export interface MediaCaptureParams {
  tabId?: number;
  includeElements?: boolean;
  full?: boolean;
}

export interface MediaCaptureResult {
  tabId: number;
  pngBase64: string;
  capturePath?: "captureVisibleTab" | "cdp";
  viewportWidth: number;
  viewportHeight: number;
  devicePixelRatio?: number;
  elements?: SnapElement[];
}

/** Multi-tier resilient selector; tiers are tried in document order below. */
export interface MacroTarget {
  testId?: string;
  ariaLabel?: string;
  role?: string;
  text?: string;
  tag?: string;
  type?: string;
  xpath?: string;
  cssSelector?: string;
  framePath?: string;
}

export interface PageDescribeParams {
  tabId?: number;
  id: number;
}

export interface PageDescribeResult {
  target: MacroTarget;
}

export interface PageResolveParams {
  tabId?: number;
  target: MacroTarget;
  timeoutMs?: number;
}

export type ResolveTier = "testId" | "ariaLabel" | "text" | "xpath" | "cssSelector";

export interface PageResolveResult {
  id: number;
  tag: string;
  type: string;
  label: string;
  x: number;
  y: number;
  w: number;
  h: number;
  matchedTier: ResolveTier;
}

export interface AuthSsoParams {
  account?: string;
  timeoutMs?: number;
}

export interface AuthSsoResult {
  success: boolean;
  selectedAccount: string;
  redirectCompleted: boolean;
  parentTabUrl: string;
}

export interface EvalParams {
  tabId?: number;
  expression: string;
  awaitPromise?: boolean;
  timeoutMs?: number;
}

export interface EvalResult {
  json: string;
  type?: string;
  truncated?: boolean;
}

export interface PageAttrParams {
  tabId?: number;
  id?: number;
  selector?: string;
  frame?: string;
}

export interface FormControlState {
  frame: string;
  tag: string;
  type: string;
  name?: string;
  id?: string;
  label?: string;
  value?: string;
  checked?: boolean;
  disabled?: boolean;
}

export interface PageFormStateParams {
  tabId?: number;
}

export interface PageFormStateResult {
  controls: FormControlState[];
}

export interface ElementRect {
  x: number;
  y: number;
  w: number;
  h: number;
}

export interface PageAttrResult {
  docId: string;
  snapshotId: number;
  tag: string;
  role?: string;
  type?: string;
  label?: string;
  value?: string;
  state?: string;
  href?: string;
  src?: string;
  name?: string;
  elementId?: string;
  rect: ElementRect;
  attributes?: Record<string, string>;
  matches: number;
}

export interface NetEntry {
  requestId?: string;
  url: string;
  method?: string;
  type?: string;
  status?: number;
  error?: string;
  startedAt?: number;
  durationMs?: number;
}

export interface PageNetParams {
  tabId?: number;
  errorsOnly?: boolean;
  limit?: number;
  sinceMs?: number;
}

export interface PageNetResult {
  entries: NetEntry[];
  enabledAt?: number;
  note?: string;
}
