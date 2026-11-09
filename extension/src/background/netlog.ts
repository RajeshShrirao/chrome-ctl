/**
 * CDP Network observation for `wait --network` and `net`.
 *
 * Resource Timing only reports completed requests, so counting in-flight ones
 * needs the debugger. The ring is capped per tab and mirrored to
 * `chrome.storage.session` so history survives service-worker suspension.
 */

import type { NetEntry } from "../shared/protocol.js";

export const MAX_NET_ENTRIES = 200;
export const DEFAULT_QUIET_MS = 500;
export const DEFAULT_LONG_POLL_MS = 2_000;
const PERSIST_THROTTLE_MS = 250;
const STORAGE_PREFIX = "netlog:";
const IGNORED_TYPES = new Set(["websocket", "eventsource"]);

export interface QuietState {
  inflight: number;
  lastActivityAt: number;
}

/** Quiet means no in-flight request and no request activity for `quietMs`. */
export class QuietTracker {
  private inflightCount = 0;
  private activityAt: number;

  constructor(now = Date.now()) {
    this.activityAt = now;
  }

  get inflight(): number {
    return this.inflightCount;
  }

  get lastActivityAt(): number {
    return this.activityAt;
  }

  noteActivity(now = Date.now()): void {
    this.activityAt = now;
  }

  setInflight(count: number): void {
    this.inflightCount = Math.max(0, count);
  }

  isQuiet(now = Date.now(), quietMs = DEFAULT_QUIET_MS): boolean {
    return this.inflightCount === 0 && now - this.activityAt >= quietMs;
  }

  state(): QuietState {
    return { inflight: this.inflightCount, lastActivityAt: this.activityAt };
  }
}

interface PendingRequest {
  url: string;
  method: string;
  type: string;
  status: number;
  startedAt: number;
}

/** One tab's request ring plus the in-flight set. */
export class NetLog {
  readonly tracker = new QuietTracker();
  entries: NetEntry[] = [];
  enabledAt = 0;
  requestsSeen = 0;

  private pending = new Map<string, PendingRequest>();

  get inflight(): number {
    return this.pending.size;
  }

  inflightExcludingLongPoll(
    now = Date.now(),
    longPollMs = DEFAULT_LONG_POLL_MS,
  ): number {
    let count = 0;
    for (const req of this.pending.values()) {
      if (now - req.startedAt < longPollMs) {
        count += 1;
      }
    }
    return count;
  }

  isIgnoredType(type: string): boolean {
    return IGNORED_TYPES.has(type.toLowerCase());
  }

  requestStart(
    requestId: string,
    url: string,
    method: string,
    type: string,
    now = Date.now(),
  ): void {
    if (this.isIgnoredType(type) || this.pending.has(requestId)) {
      return;
    }
    this.pending.set(requestId, {
      url,
      method,
      type,
      status: 0,
      startedAt: now,
    });
    this.requestsSeen += 1;
    this.tracker.setInflight(this.pending.size);
    this.tracker.noteActivity(now);
  }

  response(requestId: string, status: number, now = Date.now()): void {
    const request = this.pending.get(requestId);
    if (!request) {
      return;
    }
    request.status = status;
    this.tracker.noteActivity(now);
  }

  finish(requestId: string, now = Date.now()): void {
    this.settle(requestId, undefined, now);
  }

  fail(requestId: string, error: string, now = Date.now()): void {
    this.settle(requestId, error === "" ? "failed" : error, now);
  }

  /** Records a navigation so a stale quiet window cannot match immediately. */
  noteNavigation(now = Date.now()): void {
    this.tracker.noteActivity(now);
  }

  restore(entries: NetEntry[], enabledAt: number): void {
    if (enabledAt > 0 && this.enabledAt === 0) {
      this.enabledAt = enabledAt;
    }
    if (entries.length === 0) {
      return;
    }
    this.entries = [...entries, ...this.entries].slice(-MAX_NET_ENTRIES);
  }

  private settle(requestId: string, error: string | undefined, now: number): void {
    const request = this.pending.get(requestId);
    if (!request) {
      return;
    }
    this.pending.delete(requestId);
    this.tracker.setInflight(this.pending.size);
    this.tracker.noteActivity(now);

    const entry: NetEntry = {
      requestId,
      url: request.url,
      method: request.method,
      type: request.type,
      startedAt: request.startedAt,
      durationMs: Math.max(0, now - request.startedAt),
    };
    if (error === undefined) {
      if (request.status > 0) {
        entry.status = request.status;
      }
    } else {
      entry.error = error;
    }

    this.entries.push(entry);
    if (this.entries.length > MAX_NET_ENTRIES) {
      this.entries.splice(0, this.entries.length - MAX_NET_ENTRIES);
    }
  }
}

const logs = new Map<number, NetLog>();
const restores = new Map<number, Promise<NetLog>>();
const enabledSessions = new Set<number>();
const persistTimers = new Map<number, ReturnType<typeof setTimeout>>();
let listenerInstalled = false;

function hasChrome(): boolean {
  return typeof chrome !== "undefined" && chrome.debugger !== undefined;
}

function storageKey(tabId: number): string {
  return `${STORAGE_PREFIX}${tabId}`;
}

async function loadLog(tabId: number): Promise<NetLog> {
  const existing = logs.get(tabId);
  if (existing) {
    return existing;
  }
  const pendingRestore = restores.get(tabId);
  if (pendingRestore) {
    return pendingRestore;
  }

  const promise = (async (): Promise<NetLog> => {
    const log = new NetLog();
    logs.set(tabId, log);
    try {
      const area = chrome.storage?.session;
      if (area) {
        const key = storageKey(tabId);
        const stored = (await area.get(key)) as Record<string, unknown> | undefined;
        const record = stored?.[key] as
          | { entries?: unknown; enabledAt?: unknown }
          | undefined;
        if (record && Array.isArray(record.entries)) {
          log.restore(
            record.entries as NetEntry[],
            typeof record.enabledAt === "number" ? record.enabledAt : 0,
          );
        }
      }
    } catch {
      // Session storage is best-effort; the in-memory ring still works.
    }
    restores.delete(tabId);
    return log;
  })();

  restores.set(tabId, promise);
  return promise;
}

function schedulePersist(tabId: number): void {
  if (!hasChrome() || !chrome.storage?.session || persistTimers.has(tabId)) {
    return;
  }
  persistTimers.set(
    tabId,
    setTimeout(() => {
      persistTimers.delete(tabId);
      void flush(tabId);
    }, PERSIST_THROTTLE_MS),
  );
}

async function flush(tabId: number): Promise<void> {
  const log = logs.get(tabId);
  const area = chrome.storage?.session;
  if (!log || !area) {
    return;
  }
  try {
    await area.set({
      [storageKey(tabId)]: { entries: log.entries, enabledAt: log.enabledAt },
    });
  } catch {
    // Quota or shutdown: keep the in-memory ring.
  }
}

function handleDebuggerEvent(
  source: chrome.debugger.DebuggerSession,
  method: string,
  params?: Record<string, unknown>,
): void {
  const tabId = source.tabId;
  if (typeof tabId !== "number") {
    return;
  }
  const log = logs.get(tabId);
  if (!log) {
    return;
  }

  const now = Date.now();
  switch (method) {
    case "Network.requestWillBeSent": {
      const requestId = params?.requestId;
      const request = params?.request as
        | { url?: unknown; method?: unknown }
        | undefined;
      if (typeof requestId !== "string" || !request) {
        return;
      }
      log.requestStart(
        requestId,
        typeof request.url === "string" ? request.url : "",
        typeof request.method === "string" ? request.method : "GET",
        typeof params?.type === "string" ? params.type : "",
        now,
      );
      schedulePersist(tabId);
      return;
    }
    case "Network.responseReceived": {
      const requestId = params?.requestId;
      const response = params?.response as { status?: unknown } | undefined;
      if (typeof requestId !== "string" || !response) {
        return;
      }
      log.response(
        requestId,
        typeof response.status === "number" ? response.status : 0,
        now,
      );
      return;
    }
    case "Network.loadingFinished": {
      const requestId = params?.requestId;
      if (typeof requestId !== "string") {
        return;
      }
      log.finish(requestId, now);
      schedulePersist(tabId);
      return;
    }
    case "Network.loadingFailed": {
      const requestId = params?.requestId;
      if (typeof requestId !== "string") {
        return;
      }
      const canceled = params?.canceled === true;
      const errorText = typeof params?.errorText === "string" ? params.errorText : "";
      log.fail(
        requestId,
        canceled && errorText === "" ? "canceled" : errorText,
        now,
      );
      schedulePersist(tabId);
      return;
    }
    default:
      return;
  }
}

function ensureListener(): void {
  if (listenerInstalled || !hasChrome() || !chrome.debugger.onEvent) {
    return;
  }
  listenerInstalled = true;
  chrome.debugger.onEvent.addListener(handleDebuggerEvent);
}

// Register at module init so a resumed worker records events for any tab that
// re-attaches the debugger.
ensureListener();

/**
 * Called after the debugger attaches. Enables the Network domain once per
 * session; failures are swallowed because actions must not fail on telemetry.
 */
export async function onAttached(tabId: number): Promise<void> {
  ensureListener();
  const log = await loadLog(tabId);
  if (enabledSessions.has(tabId)) {
    return;
  }
  enabledSessions.add(tabId);
  try {
    await chrome.debugger.sendCommand({ tabId }, "Network.enable", {
      maxTotalBufferSize: 10_000_000,
      maxResourceBufferSize: 5_000_000,
    });
    // Observation starts now: without this, a wait can declare quiet while the
    // events from before Network.enable are still in flight and never seen.
    log.tracker.noteActivity();
    if (log.enabledAt === 0) {
      log.enabledAt = Date.now();
      schedulePersist(tabId);
    }
  } catch {
    enabledSessions.delete(tabId);
  }
}

export function onDebuggerDetached(tabId: number): void {
  enabledSessions.delete(tabId);
}

export function isNetworkEnabled(tabId: number): boolean {
  return enabledSessions.has(tabId);
}

export function noteNavigation(tabId: number): void {
  const log = logs.get(tabId);
  log?.noteNavigation();
}

export function forgetTab(tabId: number): void {
  logs.delete(tabId);
  restores.delete(tabId);
  enabledSessions.delete(tabId);
  const timer = persistTimers.get(tabId);
  if (timer !== undefined) {
    clearTimeout(timer);
    persistTimers.delete(tabId);
  }
  try {
    void chrome.storage?.session?.remove(storageKey(tabId));
  } catch {
    // Storage already gone.
  }
}

export interface NetSnapshot {
  entries: NetEntry[];
  enabledAt: number;
  inflight: number;
  requestsSeen: number;
  note: string;
}

const NET_NOTE =
  "collected only while the debugger is attached; enable via an action or wait --network";

export async function snapshot(tabId: number): Promise<NetSnapshot> {
  const log = await loadLog(tabId);
  return {
    entries: [...log.entries],
    enabledAt: log.enabledAt,
    inflight: log.inflight,
    requestsSeen: log.requestsSeen,
    note: NET_NOTE,
  };
}

export interface QuietWait {
  matched: boolean;
  waitedMs: number;
  inflight: number;
  requests: number;
  reason?: string;
}

export async function waitForQuiet(
  tabId: number,
  timeoutMs: number,
  quietMs: number,
  longPollMs: number = DEFAULT_LONG_POLL_MS,
): Promise<QuietWait> {
  const log = await loadLog(tabId);
  const started = Date.now();
  const baseline = log.requestsSeen;

  for (;;) {
    const now = Date.now();
    const activeInflight = log.inflightExcludingLongPoll(now, longPollMs);
    const quiet =
      activeInflight === 0 && now - log.tracker.lastActivityAt >= quietMs;
    if (quiet) {
      return {
        matched: true,
        waitedMs: now - started,
        inflight: activeInflight,
        requests: log.requestsSeen - baseline,
      };
    }
    if (now - started >= timeoutMs) {
      return {
        matched: false,
        waitedMs: now - started,
        inflight: activeInflight,
        requests: log.requestsSeen - baseline,
        reason: "timeout",
      };
    }
    await new Promise<void>((resolve) => setTimeout(resolve, 100));
  }
}
