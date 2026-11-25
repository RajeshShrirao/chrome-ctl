import type { DownloadItem, PageDownloadsResult } from "../shared/protocol.js";

const DEFAULT_LIMIT = 10;
const MAX_LIMIT = 50;
const RECENT_WINDOW_MS = 2_000;

export interface RawChromeDownload {
  id: number;
  url?: string;
  finalUrl?: string;
  filename?: string;
  mime?: string;
  fileSize?: number;
  bytesReceived?: number;
  state?: string;
  error?: string;
  startTime?: string;
}

export function normalizeDownloadItem(item: RawChromeDownload): DownloadItem {
  const bytes =
    typeof item.fileSize === "number" && item.fileSize >= 0
      ? item.fileSize
      : typeof item.bytesReceived === "number" && item.bytesReceived >= 0
        ? item.bytesReceived
        : 0;
  const startMs = item.startTime ? Date.parse(item.startTime) : Number.NaN;
  const normalized: DownloadItem = {
    id: item.id,
    url: item.finalUrl || item.url || "",
    filename: item.filename || "",
    bytes,
    state: item.state || "in_progress",
  };
  if (item.mime) {
    normalized.mime = item.mime;
  }
  if (item.error) {
    normalized.error = item.error;
  }
  if (Number.isFinite(startMs)) {
    normalized.startAt = startMs;
  }
  return normalized;
}

export async function listDownloads(limit = DEFAULT_LIMIT): Promise<PageDownloadsResult> {
  if (typeof chrome === "undefined" || !chrome.downloads?.search) {
    throw new Error("chrome.downloads API is unavailable; reload the extension");
  }
  const bounded = Math.max(1, Math.min(MAX_LIMIT, Math.round(limit)));
  const items = await chrome.downloads.search({
    orderBy: ["-startTime"],
    limit: bounded,
  });
  return {
    downloads: items.map((item) => normalizeDownloadItem(item)),
  };
}

async function fetchDownloadById(id: number): Promise<DownloadItem | undefined> {
  const items = await chrome.downloads.search({ id });
  if (items.length === 0) {
    return undefined;
  }
  return normalizeDownloadItem(items[0]);
}

/**
 * Wait for a newly started (or just-started) browser download to reach
 * `complete` or `interrupted`.
 */
export async function waitForDownload(timeoutMs: number): Promise<PageDownloadsResult> {
  if (typeof chrome === "undefined" || !chrome.downloads?.search) {
    throw new Error("chrome.downloads API is unavailable; reload the extension");
  }

  const started = Date.now();
  const since = started - RECENT_WINDOW_MS;
  const boundedTimeout = Math.max(500, Math.min(60_000, Math.round(timeoutMs)));

  // Check if a download was just kicked off right before waitForDownload was called.
  const recent = await chrome.downloads.search({
    orderBy: ["-startTime"],
    limit: 5,
  });
  const trackedIds = new Set<number>();
  for (const raw of recent) {
    const item = normalizeDownloadItem(raw);
    if (item.startAt !== undefined && item.startAt >= since) {
      if (item.state === "complete" || item.state === "interrupted") {
        return {
          matched: item.state === "complete",
          waitedMs: Date.now() - started,
          download: item,
        };
      }
      trackedIds.add(item.id);
    }
  }

  return new Promise<PageDownloadsResult>((resolve) => {
    let settled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const finish = (result: PageDownloadsResult): void => {
      if (settled) {
        return;
      }
      settled = true;
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      chrome.downloads.onCreated.removeListener(onCreated);
      chrome.downloads.onChanged.removeListener(onChanged);
      resolve(result);
    };

    const onCreated = (item: chrome.downloads.DownloadItem): void => {
      trackedIds.add(item.id);
      if (item.state === "complete" || item.state === "interrupted") {
        finish({
          matched: item.state === "complete",
          waitedMs: Date.now() - started,
          download: normalizeDownloadItem(item),
        });
      }
    };

    const onChanged = (delta: chrome.downloads.DownloadDelta): void => {
      const nextState = delta.state?.current;
      if (nextState !== "complete" && nextState !== "interrupted") {
        return;
      }
      if (trackedIds.size > 0 && !trackedIds.has(delta.id)) {
        return;
      }
      void fetchDownloadById(delta.id).then((item) => {
        finish({
          matched: nextState === "complete",
          waitedMs: Date.now() - started,
          download: item,
        });
      });
    };

    chrome.downloads.onCreated.addListener(onCreated);
    chrome.downloads.onChanged.addListener(onChanged);

    timer = setTimeout(() => {
      finish({
        matched: false,
        waitedMs: Date.now() - started,
      });
    }, boundedTimeout);
  });
}
