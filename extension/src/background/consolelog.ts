import type { ConsoleEntry } from "../shared/protocol.js";

export const MAX_CONSOLE_ENTRIES = 200;

interface RemoteArg {
  type?: string;
  value?: unknown;
  description?: string;
  unserializableValue?: string;
}

export function formatConsoleArgs(args: unknown): string {
  if (!Array.isArray(args)) {
    return "";
  }
  const parts: string[] = [];
  for (const raw of args) {
    if (typeof raw !== "object" || raw === null) {
      parts.push(String(raw));
      continue;
    }
    const arg = raw as RemoteArg;
    if (arg.value !== undefined) {
      parts.push(typeof arg.value === "string" ? arg.value : JSON.stringify(arg.value));
    } else if (typeof arg.unserializableValue === "string") {
      parts.push(arg.unserializableValue);
    } else if (typeof arg.description === "string") {
      parts.push(arg.description);
    } else if (typeof arg.type === "string") {
      parts.push(`[${arg.type}]`);
    }
  }
  return parts.join(" ").trim();
}

export function normalizeLevel(raw: string | undefined): string {
  const level = (raw ?? "log").toLowerCase();
  if (level === "warning") return "warn";
  if (level === "assert") return "error";
  return level;
}

export class ConsoleLog {
  entries: ConsoleEntry[] = [];
  enabledAt = 0;

  push(entry: ConsoleEntry): void {
    this.entries.push(entry);
    if (this.entries.length > MAX_CONSOLE_ENTRIES) {
      this.entries.splice(0, this.entries.length - MAX_CONSOLE_ENTRIES);
    }
  }

  clear(): void {
    this.entries = [];
  }
}

const logs = new Map<number, ConsoleLog>();
const enabledTabs = new Set<number>();
let listenerInstalled = false;

function hasChrome(): boolean {
  return typeof chrome !== "undefined" && chrome.debugger !== undefined;
}

function getOrCreate(tabId: number): ConsoleLog {
  let log = logs.get(tabId);
  if (!log) {
    log = new ConsoleLog();
    logs.set(tabId, log);
  }
  return log;
}

function handleEvent(
  source: chrome.debugger.DebuggerSession,
  method: string,
  params?: Record<string, unknown>,
): void {
  const tabId = source.tabId;
  if (typeof tabId !== "number") {
    return;
  }

  if (method === "Runtime.consoleAPICalled") {
    const log = getOrCreate(tabId);
    const level = normalizeLevel(typeof params?.type === "string" ? params.type : "log");
    const text = formatConsoleArgs(params?.args);
    const stack = params?.stackTrace as { callFrames?: Array<{ url?: string; lineNumber?: number }> } | undefined;
    const top = stack?.callFrames?.[0];
    log.push({
      level,
      text: text || "(empty)",
      source: "console",
      ...(top?.url ? { url: top.url } : {}),
      ...(typeof top?.lineNumber === "number" ? { line: top.lineNumber + 1 } : {}),
      timestamp: Date.now(),
    });
    return;
  }

  if (method === "Runtime.exceptionThrown") {
    const log = getOrCreate(tabId);
    const details = params?.exceptionDetails as
      | { text?: string; url?: string; lineNumber?: number; exception?: { description?: string } }
      | undefined;
    const text =
      details?.exception?.description ??
      details?.text ??
      "Uncaught exception";
    log.push({
      level: "error",
      text,
      source: "exception",
      ...(details?.url ? { url: details.url } : {}),
      ...(typeof details?.lineNumber === "number" ? { line: details.lineNumber + 1 } : {}),
      timestamp: Date.now(),
    });
    return;
  }

  if (method === "Log.entryAdded") {
    const log = getOrCreate(tabId);
    const entry = params?.entry as
      | { level?: string; text?: string; source?: string; url?: string; lineNumber?: number }
      | undefined;
    if (!entry) {
      return;
    }
    log.push({
      level: normalizeLevel(entry.level),
      text: entry.text ?? "",
      ...(entry.source ? { source: entry.source } : {}),
      ...(entry.url ? { url: entry.url } : {}),
      ...(typeof entry.lineNumber === "number" ? { line: entry.lineNumber + 1 } : {}),
      timestamp: Date.now(),
    });
  }
}

function ensureListener(): void {
  if (listenerInstalled || !hasChrome() || !chrome.debugger.onEvent) {
    return;
  }
  listenerInstalled = true;
  chrome.debugger.onEvent.addListener(handleEvent);
}

ensureListener();

export async function onConsoleAttached(tabId: number): Promise<void> {
  ensureListener();
  const log = getOrCreate(tabId);
  if (enabledTabs.has(tabId)) {
    return;
  }
  enabledTabs.add(tabId);
  try {
    await chrome.debugger.sendCommand({ tabId }, "Runtime.enable");
    await chrome.debugger.sendCommand({ tabId }, "Log.enable").catch(() => {});
    if (log.enabledAt === 0) {
      log.enabledAt = Date.now();
    }
  } catch {
    enabledTabs.delete(tabId);
  }
}

export function onConsoleDetached(tabId: number): void {
  enabledTabs.delete(tabId);
}

export function forgetConsoleTab(tabId: number): void {
  enabledTabs.delete(tabId);
  logs.delete(tabId);
}

export function clearConsole(tabId: number): void {
  logs.get(tabId)?.clear();
}

export function consoleSnapshot(tabId: number): { entries: ConsoleEntry[]; enabledAt: number } {
  const log = logs.get(tabId);
  if (!log) {
    return { entries: [], enabledAt: 0 };
  }
  return { entries: [...log.entries], enabledAt: log.enabledAt };
}
