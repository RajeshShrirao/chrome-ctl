import type { ActivityEntry, RuntimeMessage, UiStateSnapshot } from "../shared/messages.js";

function byId<T extends HTMLElement>(id: string): T {
  const el = document.getElementById(id);
  if (!el) {
    throw new Error(`missing element #${id}`);
  }
  return el as T;
}

const dot = byId<HTMLSpanElement>("dot");
const statusText = byId<HTMLSpanElement>("status-text");
const meta = byId<HTMLSpanElement>("meta");
const pauseBtn = byId<HTMLButtonElement>("pause-btn");
const focusBtn = byId<HTMLButtonElement>("focus-btn");
const detachBtn = byId<HTMLButtonElement>("detach-btn");
const clearBtn = byId<HTMLButtonElement>("clear-btn");
const banner = byId<HTMLParagraphElement>("banner");
const feedEl = byId<HTMLElement>("feed");
const emptyEl = byId<HTMLParagraphElement>("empty");

let currentPaused = false;

function send<T>(message: RuntimeMessage): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    chrome.runtime.sendMessage(message, (response: T) => {
      const runtimeError = chrome.runtime.lastError;
      if (runtimeError) {
        reject(new Error(runtimeError.message ?? "message failed"));
        return;
      }
      resolve(response);
    });
  });
}

function showBanner(text: string): void {
  banner.textContent = text;
  banner.hidden = text === "";
}

function formatClock(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function formatDuration(ms: number | undefined): string {
  if (ms === undefined) {
    return "…";
  }
  if (ms < 1000) {
    return `${ms}ms`;
  }
  return `${(ms / 1000).toFixed(1)}s`;
}

function renderEntry(entry: ActivityEntry): HTMLDivElement {
  const row = document.createElement("div");
  row.className = "entry";

  const main = document.createElement("div");
  main.className = "entry-main";

  const time = document.createElement("span");
  time.className = "time";
  time.textContent = formatClock(entry.startedAt);

  const method = document.createElement("span");
  method.className = "method";
  method.textContent = entry.method;

  const summary = document.createElement("span");
  summary.className = "summary";
  summary.textContent = entry.summary;
  summary.title = entry.summary;

  const dur = document.createElement("span");
  dur.className = "dur";
  dur.textContent = formatDuration(entry.durationMs);

  const pill = document.createElement("span");
  pill.className = `pill ${entry.status}`;
  pill.textContent = entry.status === "running" ? "run" : entry.status;

  main.append(time, method, summary, dur, pill);
  row.appendChild(main);

  if (entry.error) {
    const err = document.createElement("div");
    err.className = "entry-error";
    err.textContent = entry.error;
    row.appendChild(err);
  }

  return row;
}

function renderState(snapshot: UiStateSnapshot): void {
  currentPaused = snapshot.paused;

  let dotClass: string = snapshot.connState;
  let label = "Connected";
  if (snapshot.connState === "disconnected") {
    label = "Disconnected";
  } else if (snapshot.connState === "unconfigured") {
    label = "Waiting for daemon";
  } else if (snapshot.connState === "connecting") {
    label = "Connecting";
  } else if (snapshot.paused) {
    dotClass = "paused";
    label = "Paused";
  } else if (snapshot.inflightCount > 0) {
    dotClass = "running";
    label = `Running (${snapshot.inflightCount})`;
  }

  dot.className = `dot ${dotClass}`;
  statusText.textContent = label;

  const parts: string[] = [];
  if (snapshot.automationWindowId !== undefined) {
    parts.push(`win:${snapshot.automationWindowId}`);
  }
  if (snapshot.debuggerAttached) {
    parts.push("dbg");
  }
  meta.textContent = parts.join(" · ");

  pauseBtn.textContent = snapshot.paused ? "Resume" : "Pause";
  pauseBtn.className = snapshot.paused ? "paused" : "primary";
  pauseBtn.disabled = snapshot.connState !== "connected";
  detachBtn.disabled = !snapshot.debuggerAttached;

  showBanner(snapshot.lastError ?? "");

  const stickToBottom =
    feedEl.scrollHeight - feedEl.scrollTop - feedEl.clientHeight < 40;

  feedEl.replaceChildren();
  if (snapshot.activities.length === 0) {
    feedEl.appendChild(emptyEl);
  } else {
    const frag = document.createDocumentFragment();
    for (const entry of snapshot.activities) {
      frag.appendChild(renderEntry(entry));
    }
    feedEl.appendChild(frag);
    if (stickToBottom) {
      feedEl.scrollTop = feedEl.scrollHeight;
    }
  }
}

async function refresh(): Promise<void> {
  try {
    const snapshot = await send<UiStateSnapshot>({ target: "sw", type: "ui.getState" });
    if (snapshot) {
      renderState(snapshot);
    }
  } catch {
    showBanner("Service worker unavailable");
  }
}

pauseBtn.addEventListener("click", () => {
  pauseBtn.disabled = true;
  void send<{ ok?: boolean; error?: string }>({
    target: "sw",
    type: "ui.setPaused",
    paused: !currentPaused,
  })
    .then((res) => {
      if (!res?.ok) {
        showBanner(res?.error ?? "Failed to update pause state");
      }
      return refresh();
    })
    .catch(() => {
      showBanner("Failed to update pause state");
      pauseBtn.disabled = false;
    });
});

focusBtn.addEventListener("click", () => {
  void send<{ ok?: boolean; error?: string }>({
    target: "sw",
    type: "ui.focusAutomation",
  }).then((res) => {
    if (!res?.ok) {
      showBanner(res?.error ?? "Could not focus automation window");
    }
  });
});

detachBtn.addEventListener("click", () => {
  void send({ target: "sw", type: "ui.detachDebugger" }).then(() => refresh());
});

clearBtn.addEventListener("click", () => {
  void send({ target: "sw", type: "ui.clearFeed" }).then(() => refresh());
});

chrome.runtime.onMessage.addListener((message: unknown) => {
  if (typeof message !== "object" || message === null) {
    return false;
  }
  const msg = message as Partial<RuntimeMessage>;
  if (msg.target === "ui" && msg.type === "ui.state" && msg.snapshot) {
    renderState(msg.snapshot);
  }
  return false;
});

void send({ target: "sw", type: "ensureOffscreen" }).catch(() => {});
void send({ target: "sw", type: "ui.clearError" }).catch(() => {});
void refresh();
