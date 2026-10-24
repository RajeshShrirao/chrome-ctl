#!/usr/bin/env node
import process from "node:process";

function arg(name, fallback) {
  const index = process.argv.indexOf(name);
  return index >= 0 ? process.argv[index + 1] : fallback;
}

const addr = arg("--addr", "127.0.0.1:9229");
const token = arg("--token", "");
const tabCount = Number(arg("--tabs", "3"));

function rejectedUnauthenticated() {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://${addr}/ws`);
    const timer = setTimeout(() => reject(new Error("unauthenticated handshake did not fail")), 3000);
    ws.onopen = () => {
      clearTimeout(timer);
      ws.close();
      reject(new Error("unauthenticated handshake was accepted"));
    };
    ws.onerror = () => {
      clearTimeout(timer);
      resolve();
    };
    ws.onclose = () => {
      clearTimeout(timer);
      resolve();
    };
  });
}

function connect() {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://${addr}/ws?token=${encodeURIComponent(token)}`);
    const timer = setTimeout(() => reject(new Error("authenticated handshake timed out")), 3000);
    ws.onopen = () => {
      clearTimeout(timer);
      resolve(ws);
    };
    ws.onerror = () => {
      clearTimeout(timer);
      reject(new Error("authenticated handshake failed"));
    };
  });
}

await rejectedUnauthenticated();

const ws = await connect();
console.log("CONNECTED");

let heartbeat;
let ready = false;

function send(payload) {
  ws.send(JSON.stringify(payload));
}

ws.onmessage = (event) => {
  let message;
  try {
    message = JSON.parse(String(event.data));
  } catch {
    console.error("invalid JSON from daemon");
    return;
  }

  if (message.method === "system.ready") {
    ready = true;
    console.log("READY", JSON.stringify(message.params));
    return;
  }

  if (message.id == null) {
    return;
  }

  const result = handle(message.method, message.params ?? {});
  if (result !== undefined && result.__rpcError === true) {
    console.log("HANDLED", message.method, "-> error", result.code);
    send({
      jsonrpc: "2.0",
      id: message.id,
      error: { code: result.code, message: result.message, data: result.data },
    });
    return;
  }
  if (result === undefined) {
    console.log("HANDLED", message.method, "-> method not found");
    send({
      jsonrpc: "2.0",
      id: message.id,
      error: { code: -32601, message: "method not found" },
    });
    return;
  }
  console.log("HANDLED", message.method);
  send({ jsonrpc: "2.0", id: message.id, result });
};

function makeTab(id) {
  return {
    tabId: id,
    title: id === 1 ? "Fake Tab" : `Fake Tab ${id}`,
    url: id === 1 ? "https://example.test" : `https://example.test/${id}`,
    active: id === 1,
  };
}

// A 320x180 slate PNG; small enough to inline, large enough for the daemon's
// Set-of-Mark badges to land inside the image.
const SCREENSHOT_PNG =
  "iVBORw0KGgoAAAANSUhEUgAAAUAAAAC0CAIAAABqhmJGAAABlElEQVR42u3TAQkAAAzDsHm5gWu4f1HXMQhEQaGZPaBUJAADAwYGDAwGBgwMGBgwMBgYMDBgYDAwYGDAwICBwcCAgQEDAwYGAwMGBgwMBgYMDBgYMDAYGDAwYGDAwGBgwMCAgcHAgIEBAwMGBgMDBgYMDAZWAQwMGBgwMBgYMDBgYMDAYGDAwICBwcCAgQEDAwYGAwMGBgwMGBgMDBgYMDAYGDAwYGDAwGBgwMCAgQEDg4EBAwMGBgMDBgYMDBgYDAwYGDAwGBgwMGBgwMBgYMDAgIEBA4OBAQMDBgYDAwYGDAwYGAwMGBgwMGBgMDBgYMDAYGDAwICBAQODgQEDAwYGDAwGBgwMGBgMDBgYMDBgYDAwYGDAwGBgwMCAgQEDg4EBAwMGBgwMBgYMDBgYDAwYGDAwYGAwMGBgwMCAgcHAgIEBA4OBAQMDBgYMDAYGDAwYGAysAhgYMDBgYDAwYGDAwICBwcCAgQEDg4EBAwMGBgwMBgYMDBgYMDAYGDAwYGAwMGBgwMCAgcHAgIEBAwMGhm4PCcL8nyxcCooAAAAASUVORK5CYII=";

function captureResult(includeElements) {
  return {
    tabId: 1,
    pngBase64: SCREENSHOT_PNG,
    viewportWidth: 1280,
    viewportHeight: 720,
    devicePixelRatio: 1,
    elements: includeElements ? snapResult(false).elements : undefined,
  };
}

function snapResult(formsOnly) {
  return {
    tabId: 1,
    title: "Fake Tab",
    url: "https://example.test",
    viewport: { scrollY: 0, viewportHeight: 900, totalHeight: 4200, percent: 21 },
    elements: formsOnly
      ? [
          {
            id: 1,
            tag: "input",
            type: "text",
            label: "Search",
            value: "",
            state: "",
            x: 10,
            y: 20,
            w: 200,
            h: 32,
          },
        ]
      : [
          {
            id: 1,
            tag: "input",
            type: "text",
            label: "Search",
            value: "",
            state: "",
            x: 10,
            y: 20,
            w: 200,
            h: 32,
          },
          {
            id: 2,
            tag: "button",
            type: "",
            label: "Go",
            value: "",
            state: "",
            x: 40,
            y: 60,
            w: 80,
            h: 32,
          },
        ],
    formattedText: formsOnly
      ? 'TAB [1] Fake Tab | https://example.test\nVIEWPORT: 0-900px of 4200px (21%)\n[1] <input:text> "Search" () @10,20'
      : 'TAB [1] Fake Tab | https://example.test\nVIEWPORT: 0-900px of 4200px (21%)\n[1] <input:text> "Search" () @10,20\n[2] <button> "Go" @40,60',
  };
}

function fail(code, message, data) {
  return { __rpcError: true, code, message, data };
}

// Window 10 stands in for the user's own window: focused, already loaded.
const windows = new Map([[10, { activeTabId: 1, tabIds: [1, 2, 3] }]]);
let focusedWindowId = 10;
let nextWindowId = 11;
let nextTabId = 100;
let automationWindowId;

function windowInfo(id) {
  const window = windows.get(id);
  return {
    windowId: id,
    focused: id === focusedWindowId,
    state: "normal",
    tabCount: window.tabIds.length,
    activeTabId: window.activeTabId,
    automation: id === automationWindowId,
  };
}

function createWindow(params) {
  const focused = params.focused === true;
  const id = nextWindowId++;
  const tabId = nextTabId++;
  windows.set(id, { activeTabId: tabId, tabIds: [tabId] });
  if (focused) {
    focusedWindowId = id;
  }
  automationWindowId = id;
  return { windowId: id, activeTabId: tabId, tabCount: 1, focused, created: true };
}

function windowResult(id, created) {
  const info = windowInfo(id);
  return {
    windowId: id,
    activeTabId: info.activeTabId,
    tabCount: info.tabCount,
    focused: info.focused,
    created,
  };
}

function windowUseResult(id, cleared) {
  if (id === undefined) {
    return { windowId: null, activeTabId: 0, tabCount: 0, focused: false, cleared };
  }
  const info = windowInfo(id);
  return {
    windowId: id,
    activeTabId: info.activeTabId,
    tabCount: info.tabCount,
    focused: info.focused,
    cleared,
  };
}

function handle(method, params) {
  switch (method) {
    case "session.status":
      return {
        activeTab: { tabId: 1, title: "Fake Tab", url: "https://example.test" },
        tabCount,
        debuggerAttached: false,
      };
    case "tab.list":
      return { tabs: Array.from({ length: tabCount }, (_, index) => makeTab(index + 1)) };
    case "tab.open":
      return { tabId: 1, url: params.url, title: "Opened Page", loaded: true };
    case "tab.switch":
      return { success: true, activeTabId: params.tabId };
    case "page.snap":
      return snapResult(params.formsOnly);
    case "page.read":
      return {
        markdown: `# Fake Heading\n\nRequested: ${params.id ?? params.selector ?? "page"}`,
      };
    case "action.click": {
      const result = {
        success: true,
        action: "click",
        target: { id: params.id, tag: "button", label: "Go" },
        coordinates: { x: 40, y: 60 },
        durationMs: 340,
      };
      if (params.snap === true) {
        result.snapshot = snapResult(false);
      }
      return result;
    }
    case "action.type": {
      const result = {
        success: true,
        charsTyped: String(params.text ?? "").length,
        durationMs: 1250,
        typosInjected: 1,
        stateDelta: '[6] <button:submit> "Send" is now (enabled)',
      };
      if (params.snap === true) {
        result.snapshot = snapResult(false);
      }
      return result;
    }
    case "action.press":
      return { success: true, key: params.key };
    case "action.scroll":
      return { scrolledPixels: 720, newScrollY: 720, viewportPercent: 17 };
    case "action.upload":
      return {
        success: true,
        fileName: String(params.filePath ?? "").split(/[/\\]/).pop(),
        fileSize: 0,
      };
    case "page.wait":
      return { waitedMs: 1200, matched: true };
    case "media.capture":
      return captureResult(params.includeElements === true);
    case "page.describe": {
      const el = snapResult(false).elements.find((item) => item.id === params.id);
      if (!el) {
        return fail(-32001, `element [${params.id}] not found in the current snapshot`);
      }
      return {
        target: {
          ariaLabel: el.label,
          text: el.label,
          tag: el.tag,
          type: el.type || undefined,
          cssSelector: `#element-${el.id}`,
        },
      };
    }
    case "page.resolve": {
      const target = params.target ?? {};
      if (target.testId === "missing" || target.ariaLabel === "Missing") {
        return fail(-32001, `element "${target.ariaLabel ?? target.testId}" not found in DOM`);
      }
      const match =
        snapResult(false).elements.find(
          (item) => item.label === target.ariaLabel || item.label === target.text,
        ) ?? snapResult(false).elements[0];
      return {
        id: match.id,
        tag: match.tag,
        type: match.type,
        label: match.label,
        x: match.x,
        y: match.y,
        w: match.w,
        h: match.h,
        matchedTier: target.testId ? "testId" : "ariaLabel",
      };
    }
    case "auth.sso": {
      if (params.account === "timeout@example.test") {
        return fail(-32005, "SSO timeout: no Google OAuth popup detected");
      }
      return {
        success: true,
        selectedAccount: params.account || "default@example.test",
        redirectCompleted: true,
        parentTabUrl: "https://example.test/after-sso",
      };
    }
    case "window.list":
      return {
        windows: [...windows.keys()]
          .sort((a, b) => a - b)
          .map((id) => {
            const info = windowInfo(id);
            return {
              windowId: info.windowId,
              focused: info.focused,
              state: info.state,
              tabCount: info.tabCount,
              automation: info.automation,
            };
          }),
      };
    case "window.create":
      return createWindow(params);
    case "window.ensure":
      if (automationWindowId !== undefined && windows.has(automationWindowId)) {
        return windowResult(automationWindowId, false);
      }
      return createWindow(params);
    case "window.use": {
      if (params.windowId === undefined && params.focused !== true) {
        automationWindowId = undefined;
        return windowUseResult(undefined, true);
      }
      const target = params.focused === true ? focusedWindowId : Number(params.windowId);
      if (!windows.has(target)) {
        return fail(-32001, `window ${target} not found`);
      }
      automationWindowId = target;
      return windowUseResult(target, false);
    }
    case "window.close": {
      const id = Number(params.windowId);
      if (!windows.has(id)) {
        return fail(-32001, `window ${id} not found`);
      }
      windows.delete(id);
      if (automationWindowId === id) {
        automationWindowId = undefined;
      }
      if (focusedWindowId === id) {
        focusedWindowId = Math.min(...windows.keys());
      }
      return { success: true, windowId: id };
    }
    default:
      return undefined;
  }
}

ws.onclose = () => {
  clearInterval(heartbeat);
  console.log("CLOSED");
  if (!ready) process.exit(1);
  process.exit(0);
};

ws.onerror = () => {
  console.error("websocket error");
};

heartbeat = setInterval(() => {
  if (ws.readyState === WebSocket.OPEN) {
    send({ jsonrpc: "2.0", method: "system.ping", params: { timestamp: Math.floor(Date.now() / 1000) } });
  }
}, 2000);

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => {
    clearInterval(heartbeat);
    ws.close();
    process.exit(0);
  });
}
