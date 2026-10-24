/**
 * Background automation window state.
 *
 * The daemon and CLI are stateless, so the extension remembers which Chrome
 * window belongs to the agent. Commands without an explicit tab then resolve
 * there instead of the user's focused window, and the window is created
 * unfocused so the user's video or typing keeps focus.
 */

export const AUTOMATION_WINDOW_KEY = "chromeCtlAutomationWindow";

export async function readAutomationWindowId(): Promise<number | undefined> {
  try {
    const stored = await chrome.storage.local.get(AUTOMATION_WINDOW_KEY);
    const value = stored[AUTOMATION_WINDOW_KEY];
    return typeof value === "number" && Number.isInteger(value) ? value : undefined;
  } catch {
    return undefined;
  }
}

export async function writeAutomationWindowId(
  windowId: number | undefined,
): Promise<void> {
  try {
    if (windowId === undefined) {
      await chrome.storage.local.remove(AUTOMATION_WINDOW_KEY);
    } else {
      await chrome.storage.local.set({ [AUTOMATION_WINDOW_KEY]: windowId });
    }
  } catch {
    // Losing the hint only falls back to the focused window on the next command.
  }
}

/** Return the tracked window id only if the window still exists. */
export async function liveAutomationWindowId(): Promise<number | undefined> {
  const windowId = await readAutomationWindowId();
  if (windowId === undefined) {
    return undefined;
  }
  try {
    await chrome.windows.get(windowId);
    return windowId;
  } catch {
    await writeAutomationWindowId(undefined);
    return undefined;
  }
}

export async function activeTabIn(
  windowId: number,
): Promise<chrome.tabs.Tab | undefined> {
  try {
    const [tab] = await chrome.tabs.query({ windowId, active: true });
    return tab;
  } catch {
    return undefined;
  }
}

export async function lastFocusedActiveTab(): Promise<chrome.tabs.Tab | undefined> {
  try {
    const [tab] = await chrome.tabs.query({
      active: true,
      lastFocusedWindow: true,
    });
    return tab;
  } catch {
    return undefined;
  }
}

export async function lastFocusedWindowId(): Promise<number | undefined> {
  try {
    const window = await chrome.windows.getLastFocused();
    return typeof window.id === "number" ? window.id : undefined;
  } catch {
    return undefined;
  }
}

/** Active tab of the automation window, or undefined when no window is tracked. */
export async function automationActiveTab(): Promise<chrome.tabs.Tab | undefined> {
  const windowId = await liveAutomationWindowId();
  if (windowId === undefined) {
    return undefined;
  }
  return activeTabIn(windowId);
}
