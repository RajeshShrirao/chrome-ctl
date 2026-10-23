import { TOKEN_KEY } from "../shared/protocol.js";
import type { ConnState, RuntimeMessage, WsStatusReply } from "../shared/messages.js";

const POLL_MS = 1_000;
const NOTE_MS = 3_000;
const TOKEN_PATTERN = /^[0-9a-f]{64}$/;

const LABELS: Record<ConnState, string> = {
  connected: "Connected",
  connecting: "Connecting",
  disconnected: "Disconnected",
  unconfigured: "Not configured",
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
const reconnectButton = byId<HTMLButtonElement>("reconnect");
const note = byId<HTMLParagraphElement>("note");

let tokenPresent = false;
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
  dot.className = `dot ${status.state}`;
  statusText.textContent = LABELS[status.state];

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

async function bootstrap(): Promise<void> {
  try {
    const stored = await chrome.storage.local.get(TOKEN_KEY);
    const value = stored[TOKEN_KEY];
    tokenPresent = typeof value === "string" && value.length > 0;
  } catch {
    tokenPresent = false;
  }

  void send({ target: "sw", type: "ensureOffscreen" }).catch(() => {});

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

saveButton.addEventListener("click", () => {
  void saveToken();
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
