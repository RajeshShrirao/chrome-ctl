import { TOKEN_KEY, TOKEN_PATTERN } from "../shared/protocol.js";
import type { ConnState, RuntimeMessage, UiStateSnapshot, WsStatusReply } from "../shared/messages.js";

const POLL_MS = 1_000;
const NOTE_MS = 3_000;

const LABELS: Record<ConnState, string> = {
  connected: "Connected",
  connecting: "Connecting",
  disconnected: "Disconnected",
  unconfigured: "Waiting for daemon",
};

function byId<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (element === null) {
    throw new Error(`missing element #${id}`);
  }
  return element as T;
}

const dot = byId<HTMLSpanElement>("dot");
const statusText = byId<HTMLSpanElement>("status-text");
const versionLine = byId<HTMLParagraphElement>("version");
const errorLine = byId<HTMLParagraphElement>("error");
const tokenInput = byId<HTMLInputElement>("token");
const saveButton = byId<HTMLButtonElement>("save");
const openFeedButton = byId<HTMLButtonElement>("open-feed");
const pauseToggleButton = byId<HTMLButtonElement>("pause-toggle");
const pairButton = byId<HTMLButtonElement>("pair");
const reconnectButton = byId<HTMLButtonElement>("reconnect");
const note = byId<HTMLParagraphElement>("note");

let tokenPresent = false;
let isPaused = false;
let lastStatus: WsStatusReply | null = null;
let noteTimer: ReturnType<typeof setTimeout> | null = null;
let polling = false;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function send<T>(message: RuntimeMessage): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    chrome.runtime.sendMessage(message, (response: T) => {
      const runtimeError = chrome.runtime.lastError;
      if (runtimeError) {
        reject(new Error(runtimeError.message ?? "message failed"));
        return;
      }
      if (response === undefined) {
        reject(new Error("no response"));
        return;
      }
      resolve(response);
    });
  });
}

function setNote(text: string, ok = false): void {
  note.textContent = text;
  note.classList.toggle("ok", ok);
  if (noteTimer !== null) {
    clearTimeout(noteTimer);
  }
  noteTimer = setTimeout(() => {
    note.textContent = "";
    note.classList.remove("ok");
    noteTimer = null;
  }, NOTE_MS);
}

function render(status: WsStatusReply): void {
  lastStatus = status;
  const pairing = status.pairing === true && !status.tokenConfigured;
  dot.className = `dot ${pairing ? "connecting" : status.state}`;
  statusText.textContent = pairing ? "Pairing" : LABELS[status.state];

  if (status.daemonVersion) {
    versionLine.textContent = `Daemon v${status.daemonVersion}`;
    versionLine.hidden = false;
  } else {
    versionLine.hidden = true;
  }

  if (status.lastError) {
    errorLine.textContent = status.lastError;
    errorLine.hidden = false;
  } else {
    errorLine.hidden = true;
  }
}

async function fetchStatus(): Promise<WsStatusReply> {
  return send<WsStatusReply>({ target: "offscreen", type: "status.get" });
}

async function fetchStatusWithRetry(attempts = 5): Promise<WsStatusReply | null> {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      return await fetchStatus();
    } catch {
      if (attempt < attempts - 1) {
        await sleep(200);
      }
    }
  }
  return null;
}

function renderUnavailable(): void {
  render({
    state: tokenPresent ? "disconnected" : "unconfigured",
    tokenConfigured: tokenPresent,
    lastError: "bridge unavailable",
  });
}

async function poll(): Promise<void> {
  try {
    render(await fetchStatus());
  } catch {
    if (lastStatus !== null) {
      renderUnavailable();
    }
  }
}

async function syncPauseButton(): Promise<void> {
  try {
    const ui = await send<UiStateSnapshot>({ target: "sw", type: "ui.getState" });
    isPaused = ui.paused === true;
    pauseToggleButton.textContent = isPaused ? "Resume" : "Pause";
    pauseToggleButton.disabled = ui.connState !== "connected";
  } catch {
    pauseToggleButton.disabled = true;
  }
}

async function bootstrap(): Promise<void> {
  try {
    const stored = await chrome.storage.local.get(TOKEN_KEY);
    const value = stored[TOKEN_KEY];
    tokenPresent = typeof value === "string" && value.length > 0;
  } catch {
    tokenPresent = false;
  }

  void send({ target: "sw", type: "ensureOffscreen" }).catch(() => {});
  void syncPauseButton();

  const status = await fetchStatusWithRetry();
  if (status !== null) {
    render(status);
  } else {
    renderUnavailable();
  }

  if (!polling) {
    polling = true;
    setInterval(() => {
      void poll();
    }, POLL_MS);
  }
}

async function pairNow(): Promise<void> {
  setNote("Pairing...");
  try {
    const reply = await send<{ ok?: boolean }>({ target: "offscreen", type: "pair" });
    if (reply.ok === true) {
      setNote("Paired", true);
      await sleep(250);
      await poll();
      return;
    }
    setNote("Daemon not reachable");
  } catch {
    setNote("Bridge unavailable");
  }
}

async function saveToken(): Promise<void> {
  const value = tokenInput.value.trim();
  if (value.length === 0) {
    setNote("Enter a token first");
    return;
  }
  if (!TOKEN_PATTERN.test(value)) {
    setNote("Token must be 64 hex characters");
    return;
  }

  try {
    await chrome.storage.local.set({ [TOKEN_KEY]: value });
  } catch {
    setNote("Could not save token");
    return;
  }

  tokenPresent = true;
  tokenInput.value = "";
  setNote("Token saved", true);
}

async function reconnect(): Promise<void> {
  setNote("Reconnecting...");
  try {
    await send({ target: "offscreen", type: "reconnect" });
  } catch {
    setNote("Bridge unavailable");
    return;
  }
  await sleep(250);
  await poll();
}

openFeedButton.addEventListener("click", () => {
  void (async () => {
    try {
      const win = await chrome.windows.getLastFocused();
      if (typeof win.id === "number" && chrome.sidePanel?.open) {
        await chrome.sidePanel.open({ windowId: win.id });
        window.close();
        return;
      }
      setNote("Side Panel unavailable");
    } catch {
      setNote("Could not open Side Panel");
    }
  })();
});

pauseToggleButton.addEventListener("click", () => {
  pauseToggleButton.disabled = true;
  void send<{ ok?: boolean; paused?: boolean; error?: string }>({
    target: "sw",
    type: "ui.setPaused",
    paused: !isPaused,
  })
    .then((reply) => {
      if (reply?.ok) {
        isPaused = reply.paused === true;
        pauseToggleButton.textContent = isPaused ? "Resume" : "Pause";
        setNote(isPaused ? "Agent paused" : "Agent resumed", true);
      } else {
        setNote(reply?.error ?? "Could not toggle pause");
      }
    })
    .catch(() => {
      setNote("Bridge unavailable");
    })
    .finally(() => {
      pauseToggleButton.disabled = false;
    });
});

saveButton.addEventListener("click", () => {
  void saveToken();
});

pairButton.addEventListener("click", () => {
  void pairNow();
});

reconnectButton.addEventListener("click", () => {
  void reconnect();
});

tokenInput.addEventListener("keydown", (event) => {
  if (event.key === "Enter") {
    event.preventDefault();
    void saveToken();
  }
});

void bootstrap();
