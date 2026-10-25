/**
 * Lazy `chrome.debugger` controller for Phase 3 actions.
 *
 * Attach state is cached per tab. A command that fails because the session was
 * detached (navigation, DevTools opened, user cancel) clears the cache, re-attaches
 * once, and retries the command once.
 */

import { ErrorCodes } from "../shared/protocol.js";
import { RpcFault, errorText } from "./faults.js";
import { onAttached, onDebuggerDetached } from "./netlog.js";

const PROTOCOL_VERSION = "1.3";
const DETACH_HINT = /detach|not attached|no tab with given id|target closed|session closed/i;
const ALREADY_ATTACHED_HINT = /already attached|another debugger/i;

const attached = new Set<number>();
let detachListenerInstalled = false;

function fault(operation: string, error: unknown): RpcFault {
  return new RpcFault(
    ErrorCodes.debuggerDetached,
    `debugger ${operation} failed: ${errorText(error)}`,
  );
}

function isDetachError(error: unknown): boolean {
  return DETACH_HINT.test(errorText(error));
}

function installDetachListener(): void {
  if (detachListenerInstalled) {
    return;
  }
  if (typeof chrome === "undefined" || !chrome.debugger?.onDetach) {
    return;
  }
  detachListenerInstalled = true;
  chrome.debugger.onDetach.addListener((source) => {
    if (typeof source.tabId === "number") {
      attached.delete(source.tabId);
      onDebuggerDetached(source.tabId);
    }
  });
}

// Register once at module init so forced detaches never leave a stale cache.
installDetachListener();

export async function ensureAttached(tabId: number): Promise<void> {
  installDetachListener();
  if (attached.has(tabId)) {
    return;
  }

  try {
    await chrome.debugger.attach({ tabId }, PROTOCOL_VERSION);
  } catch (error) {
    if (!ALREADY_ATTACHED_HINT.test(errorText(error))) {
      throw fault("attach", error);
    }
    // Already attached (e.g. a previous worker instance): reuse the session.
  }

  try {
    await chrome.debugger.sendCommand(
      { tabId },
      "Input.setIgnoreInputEvents",
      { ignore: false },
    );
  } catch (error) {
    throw fault("setup", error);
  }

  attached.add(tabId);
  // Network observation is best-effort and must not fail the attach.
  await onAttached(tabId);
}

async function sendOnce<T>(
  tabId: number,
  method: string,
  params?: Record<string, unknown>,
): Promise<T | undefined> {
  return (await chrome.debugger.sendCommand({ tabId }, method, params)) as
    | T
    | undefined;
}

export async function send<T = Record<string, unknown>>(
  tabId: number,
  method: string,
  params?: Record<string, unknown>,
): Promise<T | undefined> {
  await ensureAttached(tabId);

  try {
    return await sendOnce<T>(tabId, method, params);
  } catch (error) {
    if (!isDetachError(error)) {
      throw fault("command", error);
    }

    attached.delete(tabId);
    await ensureAttached(tabId);
    try {
      return await sendOnce<T>(tabId, method, params);
    } catch (retryError) {
      throw fault("command", retryError);
    }
  }
}

export async function detach(tabId: number): Promise<void> {
  attached.delete(tabId);
  onDebuggerDetached(tabId);
  try {
    await chrome.debugger.detach({ tabId });
  } catch {
    // Not attached, or the tab is already gone: the cache is already clear.
  }
}

export function attachedTabs(): number[] {
  return [...attached];
}
