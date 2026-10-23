export const DAEMON_WS_URL = "ws://127.0.0.1:9229/ws";
export const HEARTBEAT_MS = 15_000;
export const STALE_MS = 45_000;
export const TOKEN_KEY = "chromeCtlToken";

export const Methods = {
  systemReady: "system.ready",
  systemPing: "system.ping",
  sessionStatus: "session.status",
  tabList: "tab.list",
  tabOpen: "tab.open",
  tabSwitch: "tab.switch",
  pageSnap: "page.snap",
  pageRead: "page.read",
  pageWait: "page.wait",
  actionClick: "action.click",
  actionType: "action.type",
  actionPress: "action.press",
  actionScroll: "action.scroll",
  actionUpload: "action.upload",
  mediaShot: "media.shot",
  mediaRecStart: "media.rec_start",
  mediaRecStop: "media.rec_stop",
  mediaCapture: "media.capture",
  authSSO: "auth.sso",
  pageDescribe: "page.describe",
  pageResolve: "page.resolve",
} as const;

export const ErrorCodes = {
  connection: -32000,
  elementNotFound: -32001,
  elementNotInteractable: -32002,
  tabNotFound: -32003,
  debuggerDetached: -32004,
  ssoTimeout: -32005,
  unauthorized: -32006,
  navigationFailed: -32007,
  macroStepFailed: -32008,
  methodNotFound: -32601,
  internal: -32603,
} as const;

export const DEFAULT_RPC_TIMEOUT_MS = 10_000;

/** Actions can take minutes (humanized typing); every hop must use the same budget. */
export function rpcTimeoutMs(method: string): number {
  switch (method) {
    case Methods.actionType:
      return 300_000;
    case Methods.actionClick:
    case Methods.actionScroll:
    case Methods.actionUpload:
      return 60_000;
    case Methods.actionPress:
      return 20_000;
    case Methods.pageWait:
      return 30_000;
    case Methods.mediaCapture:
      return 30_000;
    case Methods.authSSO:
      return 60_000;
    case Methods.pageResolve:
      return 20_000;
    case Methods.pageDescribe:
      return 5_000;
    default:
      return DEFAULT_RPC_TIMEOUT_MS;
  }
}

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

export interface ViewportInfo {
  scrollY: number;
  viewportHeight: number;
  totalHeight: number;
  percent: number;
}

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
}

export interface SnapResult {
  tabId: number;
  title: string;
  url: string;
  viewport: ViewportInfo;
  elements: SnapElement[];
  formattedText: string;
}

export interface PageReadResult {
  markdown: string;
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
  id: number;
  doubleClick?: boolean;
  snap?: boolean;
}

export interface ActionClickResult {
  success: boolean;
  action: string;
  target: ActionTarget;
  coordinates: ActionCoordinates;
  durationMs: number;
  snapshot?: SnapResult;
}

export interface ActionTypeParams {
  tabId?: number;
  id: number;
  text: string;
  enter?: boolean;
  typoRate?: number;
  snap?: boolean;
}

export interface ActionTypeResult {
  success: boolean;
  charsTyped: number;
  durationMs: number;
  typosInjected: number;
  stateDelta?: string;
  snapshot?: SnapResult;
}

export interface ActionPressParams {
  tabId?: number;
  key: string;
  modifiers?: string[];
}

export interface ActionPressResult {
  success: boolean;
  key: string;
}

export interface ActionScrollParams {
  tabId?: number;
  direction: "down" | "up";
  amount: number;
}

export interface ActionScrollResult {
  scrolledPixels: number;
  newScrollY: number;
  viewportPercent: number;
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
  timeoutMs?: number;
}

export interface PageWaitResult {
  waitedMs: number;
  matched: boolean;
}

export interface MediaCaptureParams {
  tabId?: number;
  includeElements?: boolean;
}

export interface MediaCaptureResult {
  tabId: number;
  pngBase64: string;
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
