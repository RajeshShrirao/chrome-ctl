import type { ActivityEntry, ConnState, RuntimeMessage, UiStateSnapshot } from "../shared/messages.js";

export const MAX_ACTIVITY_ENTRIES = 100;
const SESSION_KEY = "chromeCtlUiActivity";

export interface BadgeSpec {
  text: string;
  color: string;
}

export interface BadgeInput {
  connState: ConnState;
  paused: boolean;
  inflightCount: number;
  hasError: boolean;
}

/**
 * Compute toolbar badge with strict precedence:
 * 1. disconnected / unconfigured -> red "!"
 * 2. paused                      -> amber "||"
 * 3. inflight RPC                -> blue "RUN"
 * 4. recent RPC error            -> red "ERR"
 * 5. idle & connected            -> clear
 */
export function computeBadge(input: BadgeInput): BadgeSpec {
  if (input.connState === "disconnected" || input.connState === "unconfigured") {
    return { text: "!", color: "#f85149" };
  }
  if (input.paused) {
    return { text: "||", color: "#d29922" };
  }
  if (input.inflightCount > 0) {
    return { text: "RUN", color: "#4c8dff" };
  }
  if (input.hasError) {
    return { text: "ERR", color: "#f85149" };
  }
  return { text: "", color: "#3fb950" };
}

function truncate(text: string, max: number): string {
  const clean = text.replace(/\s+/g, " ").trim();
  if (clean.length <= max) {
    return clean;
  }
  return `${clean.slice(0, Math.max(1, max - 1))}…`;
}

function shortHost(raw: string): string {
  try {
    const parsed = new URL(raw);
    const path = parsed.pathname === "/" ? "" : parsed.pathname;
    return truncate(`${parsed.host}${path}`, 42);
  } catch {
    return truncate(raw, 42);
  }
}

/** Build a compact, human-readable 1-line summary for an RPC call. */
export function summarizeRpc(method: string, params: Record<string, unknown>): string {
  switch (method) {
    case "tab.open":
      return typeof params.url === "string" ? shortHost(params.url) : "open";
    case "tab.switch":
      return typeof params.tabId === "number" ? `tab [${params.tabId}]` : "switch";
    case "action.click": {
      if (typeof params.id === "number" && params.id > 0) {
        return `[${params.id}]${params.doubleClick === true ? " (2x)" : ""}`;
      }
      if (typeof params.text === "string" && params.text.trim() !== "") {
        return `"${truncate(params.text, 28)}"`;
      }
      if (typeof params.selector === "string" && params.selector.trim() !== "") {
        return truncate(params.selector, 32);
      }
      const at = params.at as { x?: unknown; y?: unknown } | undefined;
      if (typeof at?.x === "number" && typeof at?.y === "number") {
        return `@${Math.round(at.x)},${Math.round(at.y)}`;
      }
      return "click";
    }
    case "action.type": {
      const id = typeof params.id === "number" ? `[${params.id}] ` : "";
      const len = typeof params.text === "string" ? params.text.length : 0;
      const enter = params.enter === true ? " +Enter" : "";
      return `${id}${len} chars${enter}`;
    }
    case "action.press": {
      const key = typeof params.key === "string" ? params.key : "";
      const mods = Array.isArray(params.modifiers)
        ? params.modifiers.filter((m): m is string => typeof m === "string")
        : [];
      return mods.length > 0 ? `${mods.join("+")}+${key}` : key;
    }
    case "action.scroll": {
      if (typeof params.toSelector === "string" && params.toSelector.trim() !== "") {
        return `to ${truncate(params.toSelector, 28)}`;
      }
      const dir = typeof params.direction === "string" ? params.direction : "down";
      const amt = typeof params.amount === "number" ? ` ${params.amount}` : "";
      return `${dir}${amt}`;
    }
    case "action.upload": {
      const id = typeof params.id === "number" ? `[${params.id}] ` : "";
      const file = typeof params.filePath === "string" ? params.filePath.split("/").pop() ?? "" : "";
      return `${id}${truncate(file, 28)}`;
    }
    case "page.snap": {
      const flags: string[] = [];
      if (params.diff === true) flags.push("--diff");
      if (params.all === true) flags.push("--all");
      if (params.formsOnly === true) flags.push("--forms");
      if (params.deep === true) flags.push("--deep");
      if (params.frames === true) flags.push("--frames");
      return flags.length > 0 ? flags.join(" ") : "viewport";
    }
    case "page.read": {
      if (typeof params.mode === "string") return `--${params.mode}`;
      if (typeof params.id === "number") return `[${params.id}]`;
      if (typeof params.selector === "string") return truncate(params.selector, 28);
      return "markdown";
    }
    case "page.wait": {
      const parts: string[] = [];
      if (params.network === true) parts.push("network");
      if (typeof params.text === "string" && params.text !== "") {
        parts.push(`"${truncate(params.text, 22)}"`);
      }
      if (typeof params.selector === "string" && params.selector !== "") {
        parts.push(truncate(params.selector, 22));
      }
      if (params.idle === true) parts.push("idle");
      return parts.length > 0 ? parts.join(" + ") : "wait";
    }
    case "page.eval":
      return typeof params.expression === "string" ? truncate(params.expression, 36) : "eval";
    case "media.capture":
      return params.full === true ? "full-page" : params.includeElements === true ? "som" : "viewport";
    case "window.use":
      return params.focused === true
        ? "focused"
        : typeof params.windowId === "number"
          ? `win ${params.windowId}`
          : "clear";
    case "page.net_body":
      return typeof params.requestId === "string" ? truncate(params.requestId, 24) : "body";
    case "page.net_block": {
      const patterns = Array.isArray(params.patterns) ? params.patterns.length : 0;
      return patterns === 0 ? "clear" : `${patterns} patterns`;
    }
    case "page.downloads":
      return params.wait === true ? "wait" : "list";
    case "page.dialog":
      return typeof params.action === "string" && params.action !== ""
        ? params.action
        : typeof params.auto === "string" && params.auto !== ""
          ? `auto=${params.auto}`
          : "status";
    case "page.console":
      return params.clear === true ? "clear" : params.errorsOnly === true ? "--errors" : "log";
    case "page.storage":
      return `${params.area === "session" ? "session" : "local"} ${typeof params.action === "string" ? params.action : "list"}`;
    case "page.cookies":
      return typeof params.action === "string" ? params.action : "list";
    case "tab.close":
      return typeof params.tabId === "number" ? `tab [${params.tabId}]` : "close";
    case "action.hover":
      return typeof params.id === "number" && params.id > 0
        ? `[${params.id}]`
        : typeof params.text === "string"
          ? `"${truncate(params.text, 24)}"`
          : "hover";
    case "action.drag":
      return `[${String(params.fromId ?? "")}] -> [${String(params.toId ?? "")}]`;
    case "action.select":
      return `[${String(params.id ?? "")}] "${truncate(String(params.value ?? ""), 24)}"`;
    default:
      return "";
  }
}

export class ActivityFeed {
  private entries: ActivityEntry[] = [];
  private readonly maxEntries: number;

  constructor(maxEntries = MAX_ACTIVITY_ENTRIES) {
    this.maxEntries = maxEntries;
  }

  load(entries: ActivityEntry[]): void {
    this.entries = entries.slice(-this.maxEntries);
  }

  list(): ActivityEntry[] {
    return [...this.entries];
  }

  clear(): void {
    this.entries = [];
  }

  start(entry: Omit<ActivityEntry, "status">): ActivityEntry {
    const full: ActivityEntry = { ...entry, status: "running" };
    this.entries.push(full);
    if (this.entries.length > this.maxEntries) {
      this.entries.splice(0, this.entries.length - this.maxEntries);
    }
    return full;
  }

  finish(id: string, ok: boolean, durationMs: number, error?: string): ActivityEntry | undefined {
    for (let i = this.entries.length - 1; i >= 0; i -= 1) {
      const item = this.entries[i];
      if (item.id === id) {
        item.status = ok ? "ok" : "error";
        item.durationMs = Math.max(0, Math.round(durationMs));
        if (!ok && error) {
          item.error = error;
        }
        return item;
      }
    }
    return undefined;
  }
}

export class UiStateController {
  readonly feed = new ActivityFeed();
  private connState: ConnState = "unconfigured";
  private paused = false;
  private inflightCount = 0;
  private hasError = false;
  private lastError: string | undefined;
  private automationWindowId: number | undefined;
  private debuggerAttached = false;
  private hydrated = false;

  async hydrate(): Promise<void> {
    if (this.hydrated) {
      return;
    }
    this.hydrated = true;
    try {
      if (typeof chrome !== "undefined" && chrome.storage?.session) {
        const stored = await chrome.storage.session.get(SESSION_KEY);
        const raw = stored[SESSION_KEY];
        if (Array.isArray(raw)) {
          this.feed.load(raw as ActivityEntry[]);
        }
      }
    } catch {
      // Best effort; session storage may not exist in unit tests.
    }
  }

  private persistFeed(): void {
    try {
      if (typeof chrome !== "undefined" && chrome.storage?.session) {
        void chrome.storage.session.set({ [SESSION_KEY]: this.feed.list() }).catch(() => {});
      }
    } catch {
      // Ignore in non-extension test environments.
    }
  }

  snapshot(): UiStateSnapshot {
    return {
      connState: this.connState,
      paused: this.paused,
      inflightCount: this.inflightCount,
      hasError: this.hasError,
      ...(this.lastError ? { lastError: this.lastError } : {}),
      ...(this.automationWindowId !== undefined
        ? { automationWindowId: this.automationWindowId }
        : {}),
      debuggerAttached: this.debuggerAttached,
      activities: this.feed.list(),
    };
  }

  isPaused(): boolean {
    return this.paused;
  }

  isRunning(): boolean {
    return this.inflightCount > 0;
  }

  setConnState(state: ConnState, lastError?: string): void {
    this.connState = state;
    this.lastError = lastError;
    if (state === "connected") {
      this.lastError = undefined;
    }
    this.syncBadgeAndBroadcast();
  }

  setPaused(paused: boolean): void {
    this.paused = paused;
    this.syncBadgeAndBroadcast();
  }

  setAutomationWindowId(windowId: number | undefined): void {
    if (this.automationWindowId === windowId) {
      return;
    }
    this.automationWindowId = windowId;
    this.broadcast();
  }

  setDebuggerAttached(attached: boolean): void {
    if (this.debuggerAttached === attached) {
      return;
    }
    this.debuggerAttached = attached;
    this.broadcast();
  }

  clearError(): void {
    if (!this.hasError) {
      return;
    }
    this.hasError = false;
    this.syncBadgeAndBroadcast();
  }

  clearFeed(): void {
    this.feed.clear();
    this.hasError = false;
    this.persistFeed();
    this.syncBadgeAndBroadcast();
  }

  recordStart(id: string, method: string, params: Record<string, unknown>, tabId?: number): void {
    this.inflightCount += 1;
    this.feed.start({
      id,
      method,
      summary: summarizeRpc(method, params),
      tabId,
      startedAt: Date.now(),
    });
    this.persistFeed();
    this.syncBadgeAndBroadcast();
  }

  recordFinish(id: string, ok: boolean, durationMs: number, error?: string): void {
    this.inflightCount = Math.max(0, this.inflightCount - 1);
    this.feed.finish(id, ok, durationMs, error);
    this.hasError = !ok;
    this.persistFeed();
    this.syncBadgeAndBroadcast();
  }

  syncBadge(): void {
    if (typeof chrome === "undefined" || !chrome.action) {
      return;
    }
    const badge = computeBadge({
      connState: this.connState,
      paused: this.paused,
      inflightCount: this.inflightCount,
      hasError: this.hasError,
    });
    try {
      void chrome.action.setBadgeText({ text: badge.text });
      if (badge.text !== "") {
        void chrome.action.setBadgeBackgroundColor({ color: badge.color });
      }
    } catch {
      // Ignore if action API is unavailable.
    }
  }

  private broadcast(): void {
    if (typeof chrome === "undefined" || !chrome.runtime?.sendMessage) {
      return;
    }
    const msg: RuntimeMessage = {
      target: "ui",
      type: "ui.state",
      snapshot: this.snapshot(),
    };
    try {
      chrome.runtime.sendMessage(msg, () => {
        void chrome.runtime.lastError;
      });
    } catch {
      // No UI listener open; ignore.
    }
  }

  private syncBadgeAndBroadcast(): void {
    this.syncBadge();
    this.broadcast();
  }
}
