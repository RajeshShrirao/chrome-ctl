/**
 * Phase 5 Google SSO interceptor.
 *
 * Detects an accounts.google.com popup/tab, clicks the requested account row and
 * the consent/continue button, then confirms the popup left Google and the
 * parent tab finished redirecting. It never types credentials or fills fields,
 * and never logs URLs, query strings, tokens, or account emails.
 */

import { ErrorCodes } from "../shared/protocol.js";
import type { AuthSsoResult } from "../shared/protocol.js";
import { RpcFault } from "./faults.js";

const DEFAULT_TIMEOUT_MS = 15_000;
const MIN_TIMEOUT_MS = 1_000;
const MAX_TIMEOUT_MS = 60_000;
const POPUP_POLL_MS = 250;
const CLICK_INTERVAL_MS = 400;
const PARENT_REDIRECT_TIMEOUT_MS = 5_000;
const GOOGLE_ACCOUNTS_ORIGIN = "https://accounts.google.com";

interface SsoClickResult {
  password?: boolean;
  clicked?: string;
}

function sleep(ms: number): Promise<void> {
  const delay = Math.max(0, Math.round(ms));
  if (delay === 0) {
    return Promise.resolve();
  }
  return new Promise((resolve) => setTimeout(resolve, delay));
}

function clampTimeout(value: unknown): number {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return DEFAULT_TIMEOUT_MS;
  }
  return Math.max(MIN_TIMEOUT_MS, Math.min(MAX_TIMEOUT_MS, Math.round(value)));
}

function isGoogleAccountsUrl(url: string | undefined): boolean {
  if (!url) {
    return false;
  }
  return url === GOOGLE_ACCOUNTS_ORIGIN || url.startsWith(`${GOOGLE_ACCOUNTS_ORIGIN}/`);
}

function extractEmail(value: string): string {
  const match = value.match(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/);
  return match ? match[0] : "";
}

/**
 * Self-contained: Chrome serializes this function into the popup tab, so it must
 * not reference any module-scope helpers or constants.
 */
function clickGoogleSurface(account: string | null): SsoClickResult {
  const norm = (value: string | null | undefined): string =>
    (value ?? "").replace(/\s+/g, " ").trim();

  const isVisible = (el: Element): boolean => {
    if (!el.isConnected) {
      return false;
    }
    if (el.getClientRects().length === 0) {
      return false;
    }
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  };

  const isEnabled = (el: Element): boolean => {
    const control = el as HTMLElement & { disabled?: boolean };
    return control.disabled !== true && el.getAttribute("aria-disabled") !== "true";
  };

  const textOf = (el: Element): string => {
    const inner = norm((el as HTMLElement).innerText);
    return inner || norm(el.textContent);
  };

  const click = (el: Element): void => {
    (el as HTMLElement).click();
  };

  for (const password of Array.from(
    document.querySelectorAll('input[type="password"]'),
  )) {
    if (isVisible(password)) {
      return { password: true };
    }
  }

  let clicked = "";

  if (account) {
    const wanted = account.toLowerCase();
    const selectors = [
      "[data-identifier]",
      "[data-authuser]",
      "li",
      'div[role="button"]',
    ];
    let matched: Element | null = null;
    for (const selector of selectors) {
      for (const el of Array.from(document.querySelectorAll(selector))) {
        if (!isVisible(el) || !isEnabled(el)) {
          continue;
        }
        const rowText = textOf(el).toLowerCase();
        const identifier = (
          el.getAttribute("data-identifier") ??
          el.getAttribute("data-authuser") ??
          ""
        ).toLowerCase();
        if (rowText.includes(wanted) || identifier === wanted) {
          matched = el;
          break;
        }
      }
      if (matched !== null) {
        break;
      }
    }
    if (matched !== null) {
      clicked =
        textOf(matched) ||
        matched.getAttribute("data-identifier") ||
        matched.getAttribute("data-authuser") ||
        "";
      click(matched);
    }
  } else {
    for (const el of Array.from(document.querySelectorAll("[data-identifier]"))) {
      if (isVisible(el) && isEnabled(el)) {
        clicked = textOf(el) || el.getAttribute("data-identifier") || "";
        click(el);
        break;
      }
    }
  }

  for (const el of Array.from(
    document.querySelectorAll('button, a, [role="button"]'),
  )) {
    if (!isVisible(el) || !isEnabled(el)) {
      continue;
    }
    if (!/^(continue|allow|accept|confirm)$/i.test(textOf(el))) {
      continue;
    }
    const label = textOf(el);
    click(el);
    if (clicked === "") {
      clicked = label;
    }
    break;
  }

  return { clicked };
}

async function clickInGoogleTab(
  tabId: number,
  account: string | undefined,
): Promise<SsoClickResult> {
  const results = await chrome.scripting.executeScript<
    [string | null],
    SsoClickResult
  >({
    target: { tabId },
    func: clickGoogleSurface,
    args: [account ?? null],
  });

  const value = results[0]?.result;
  if (!value || typeof value !== "object") {
    return { clicked: "" };
  }
  return value;
}

/** Prefer a popup window, otherwise the first matching tab. */
async function findOAuthTab(): Promise<chrome.tabs.Tab | null> {
  let tabs: chrome.tabs.Tab[];
  try {
    tabs = await chrome.tabs.query({});
  } catch {
    return null;
  }

  const matches = tabs.filter(
    (tab) => isGoogleAccountsUrl(tab.url) || isGoogleAccountsUrl(tab.pendingUrl),
  );
  if (matches.length === 0) {
    return null;
  }

  for (const tab of matches) {
    if (typeof tab.windowId !== "number") {
      continue;
    }
    try {
      const windowInfo = await chrome.windows.get(tab.windowId);
      if (windowInfo.type === "popup") {
        return tab;
      }
    } catch {
      // The window may have vanished between query and lookup.
    }
  }
  return matches[0];
}

async function activeTabId(): Promise<number | undefined> {
  try {
    const [active] = await chrome.tabs.query({
      active: true,
      lastFocusedWindow: true,
    });
    return typeof active?.id === "number" ? active.id : undefined;
  } catch {
    return undefined;
  }
}

interface PopupState {
  closed: boolean;
  url: string;
}

async function popupState(tabId: number): Promise<PopupState> {
  try {
    const tab = await chrome.tabs.get(tabId);
    return { closed: false, url: tab.url ?? "" };
  } catch {
    return { closed: true, url: "" };
  }
}

/** Reads the parent URL, or `null` when the tab is gone/unreadable. */
async function readTabUrl(tabId: number): Promise<string | null> {
  try {
    const tab = await chrome.tabs.get(tabId);
    return tab.url ?? "";
  } catch {
    return null;
  }
}

async function waitForParentUrl(parentTabId: number | undefined): Promise<string> {
  if (parentTabId === undefined) {
    return "";
  }

  const deadline = Date.now() + PARENT_REDIRECT_TIMEOUT_MS;
  let url = await readTabUrl(parentTabId);
  while (url !== null && isGoogleAccountsUrl(url) && Date.now() < deadline) {
    await sleep(POPUP_POLL_MS);
    url = await readTabUrl(parentTabId);
  }
  return url ?? "";
}

export async function sso(
  params: Record<string, unknown> = {},
): Promise<AuthSsoResult> {
  const account =
    typeof params.account === "string" && params.account.trim() !== ""
      ? params.account.trim()
      : undefined;
  const timeoutMs = clampTimeout(params.timeoutMs);
  const deadline = Date.now() + timeoutMs;

  // The opener/parent is the active tab before any OAuth surface appears.
  const parentTabId = await activeTabId();

  let oauthTab: chrome.tabs.Tab | null = null;
  while (Date.now() < deadline) {
    oauthTab = await findOAuthTab();
    if (oauthTab !== null) {
      break;
    }
    await sleep(POPUP_POLL_MS);
  }

  if (oauthTab === null || typeof oauthTab.id !== "number") {
    throw new RpcFault(
      ErrorCodes.ssoTimeout,
      "SSO timeout: no Google OAuth popup detected",
    );
  }

  const popupTabId = oauthTab.id;
  let clickedAny = false;
  let clickedEmail = "";

  const succeed = async (): Promise<AuthSsoResult> => ({
    success: true,
    selectedAccount: clickedEmail || account || "",
    redirectCompleted: true,
    parentTabUrl: await waitForParentUrl(parentTabId),
  });

  while (Date.now() < deadline) {
    // Re-check before injecting: the popup may have completed or closed while
    // we were between iterations, and we must never click a non-Google page.
    const before = await popupState(popupTabId);
    if (before.closed || (before.url !== "" && !isGoogleAccountsUrl(before.url))) {
      if (!clickedAny) {
        throw new RpcFault(
          ErrorCodes.ssoTimeout,
          "SSO timeout: Google OAuth popup did not complete",
        );
      }
      return await succeed();
    }

    let result: SsoClickResult | null = null;
    try {
      result = await clickInGoogleTab(popupTabId, account);
    } catch {
      // The popup navigated or closed mid-click; the state check below decides.
      result = null;
    }

    if (result?.password === true) {
      throw new RpcFault(
        ErrorCodes.ssoTimeout,
        "SSO aborted: password required; complete sign-in manually",
      );
    }

    if (result !== null && typeof result.clicked === "string" && result.clicked !== "") {
      clickedAny = true;
      const email = extractEmail(result.clicked);
      if (clickedEmail === "" && email !== "") {
        clickedEmail = email;
      }
    }

    const state = await popupState(popupTabId);
    const leftGoogle =
      state.url !== "" && !isGoogleAccountsUrl(state.url);
    if (state.closed || leftGoogle) {
      if (!clickedAny) {
        throw new RpcFault(
          ErrorCodes.ssoTimeout,
          "SSO timeout: Google OAuth popup did not complete",
        );
      }
      return await succeed();
    }

    const remaining = deadline - Date.now();
    if (remaining <= 0) {
      break;
    }
    await sleep(Math.min(CLICK_INTERVAL_MS, remaining));
  }

  throw new RpcFault(
    ErrorCodes.ssoTimeout,
    "SSO timeout: Google OAuth popup did not complete",
  );
}
