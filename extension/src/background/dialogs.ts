import type { DialogInfo } from "../shared/protocol.js";

export type DialogAutoMode = "accept" | "dismiss";

export class DialogTracker {
  private readonly active = new Map<number, DialogInfo>();
  private readonly autoMode = new Map<number, DialogAutoMode>();

  noteOpening(tabId: number, info: DialogInfo): DialogAutoMode | undefined {
    this.active.set(tabId, info);
    return this.autoMode.get(tabId);
  }

  noteClosed(tabId: number): void {
    this.active.delete(tabId);
  }

  getActive(tabId: number): DialogInfo | undefined {
    return this.active.get(tabId);
  }

  getAuto(tabId: number): DialogAutoMode | undefined {
    return this.autoMode.get(tabId);
  }

  setAuto(tabId: number, mode: DialogAutoMode | "off"): void {
    if (mode === "off") {
      this.autoMode.delete(tabId);
    } else {
      this.autoMode.set(tabId, mode);
    }
  }

  forget(tabId: number): void {
    this.active.delete(tabId);
    this.autoMode.delete(tabId);
  }
}

export const tracker = new DialogTracker();
const pageEnabledTabs = new Set<number>();
let listenerInstalled = false;

function hasChrome(): boolean {
  return typeof chrome !== "undefined" && chrome.debugger !== undefined;
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

  if (method === "Page.javascriptDialogOpening") {
    const info: DialogInfo = {
      type: typeof params?.type === "string" ? params.type : "alert",
      message: typeof params?.message === "string" ? params.message : "",
      ...(typeof params?.defaultPrompt === "string" && params.defaultPrompt !== ""
        ? { defaultPrompt: params.defaultPrompt }
        : {}),
      openedAt: Date.now(),
    };
    const auto = tracker.noteOpening(tabId, info);
    if (auto) {
      void chrome.debugger
        .sendCommand({ tabId }, "Page.handleJavaScriptDialog", {
          accept: auto === "accept",
        })
        .then(() => tracker.noteClosed(tabId))
        .catch(() => {});
    }
    return;
  }

  if (method === "Page.javascriptDialogClosed") {
    tracker.noteClosed(tabId);
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

export async function onPageAttached(tabId: number): Promise<void> {
  ensureListener();
  if (pageEnabledTabs.has(tabId)) {
    return;
  }
  pageEnabledTabs.add(tabId);
  try {
    await chrome.debugger.sendCommand({ tabId }, "Page.enable");
  } catch {
    pageEnabledTabs.delete(tabId);
  }
}

export function onDialogDetached(tabId: number): void {
  pageEnabledTabs.delete(tabId);
  tracker.noteClosed(tabId);
}

export function forgetDialogTab(tabId: number): void {
  pageEnabledTabs.delete(tabId);
  tracker.forget(tabId);
}
