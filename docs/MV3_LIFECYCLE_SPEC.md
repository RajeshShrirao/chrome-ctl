# Manifest V3 Lifecycle & Debugger Architecture Spec

**Document Version:** 1.0  
**Target:** Chrome Extension Manifest V3  
**Compatibility:** Google Chrome v116+ (requires `chrome.offscreen` API)  

---

## 1. The MV3 Service Worker Challenge

Under Chrome Extension Manifest V3 (MV3):
* Background scripts run as ephemeral **Service Workers** (`service_worker.js`).
* Chrome aggressively suspends and terminates service workers when they remain idle for **30 seconds** or after **5 minutes** of continuous execution.
* If a WebSocket connection to `127.0.0.1:9229` is hosted directly in the service worker, Chrome terminates the worker while an AI agent is thinking or waiting, abruptly killing the socket.

---

## 2. The Chosen Solution: Offscreen Document WebSocket Host

To guarantee an unbroken, permanent connection to the local `chrome-ctl` daemon without requiring fragile OS-level Native Messaging hosts:

```
+--------------------------------------------------------------------------------+
|                         Go Daemon (127.0.0.1:9229)                             |
+------------------------------------+-------------------------------------------+
                                     ^
                                     |  Permanent WebSocket (Heartbeat 15s)
                                     v
+--------------------------------------------------------------------------------+
|                       Chrome Extension (Manifest V3)                           |
|                                                                                |
|  +--------------------------------------------------------------------------+  |
|  |             Offscreen Document (`src/offscreen/offscreen.html`)          |  |
|  |                                                                          |  |
|  |  - Created on extension launch via `chrome.offscreen.createDocument()`   |  |
|  |  - Owns persistent WebSocket client instance                             |  |
|  |  - Dispatches 15s ping/pong heartbeats to keep socket hot                |  |
|  |  - Relays incoming RPC calls to Service Worker via runtime messaging     |  |
|  +-------------------------------------+------------------------------------+  |
|                                        |                                       |
|                `chrome.runtime.sendMessage` (Internal Bus)                    |
|                                        v                                       |
|  +--------------------------------------------------------------------------+  |
|  |             Background Service Worker (`src/background/service_worker.ts)|  |
|  |                                                                          |  |
|  |  - Tab Management (`chrome.tabs`)                                        |  |
|  |  - CDP Debugger Host (`chrome.debugger`)                                 |  |
|  |  - Routes DOM inspection commands to Content Script                      |  |
|  +-------------------+-----------------------------------+------------------+  |
|                      |                                   |                     |
|        `chrome.tabs.sendMessage`               `chrome.debugger.sendCommand`  |
|                      v                                   v                     |
|  +-----------------------------------+   +----------------------------------+  |
|  |          Content Script           |   |       Chrome DevTools Protocol   |  |
|  |    (In-Page DOM Distillation)     |   |    (Trusted Hardware Inputs)     |  |
|  +-----------------------------------+   +----------------------------------+  |
+--------------------------------------------------------------------------------+
```

### 2.1 Offscreen Document Creation Protocol
On extension initialization (`chrome.runtime.onInstalled` and `chrome.runtime.onStartup`), the service worker creates the offscreen document if it does not already exist:

```typescript
// src/background/service_worker.ts
const OFFSCREEN_PATH = 'src/offscreen/offscreen.html';

async function ensureOffscreenDocument(): Promise<void> {
  const existingContexts = await chrome.runtime.getContexts({
    contextTypes: [chrome.runtime.ContextType.OFFSCREEN_DOCUMENT],
    documentUrls: [chrome.runtime.getURL(OFFSCREEN_PATH)],
  });

  if (existingContexts.length > 0) {
    return; // Already running
  }

  await chrome.offscreen.createDocument({
    url: OFFSCREEN_PATH,
    reasons: [chrome.offscreen.Reason.BLOBS, chrome.offscreen.Reason.MATCH_MEDIA],
    justification: 'Maintain continuous authenticated WebSocket connection to local chrome-ctl daemon',
  });
}
```

### 2.2 Reconnection & Exponential Backoff
Inside `offscreen.ts`:
* If the Go daemon terminates or restarts, the offscreen document attempts to reconnect using an exponential backoff schedule:
  $$\Delta t = \min(1000 \times 1.5^{\text{attempt}}, 10000)\text{ ms}$$
* Jitter: $\pm 20\%$ to avoid socket hammering.
* Once the daemon restarts, the extension automatically re-authenticates and enters the `ready` state without user interaction.

---

## 3. Chrome Debugger (`chrome.debugger`) Management

The `chrome.debugger` API gives `chrome-ctl` the ability to dispatch **trusted** hardware events (`event.isTrusted = true`) via the Chrome DevTools Protocol (CDP).

### 3.1 Attachment Strategy: Lazy & Auto-Tracking
* **Do NOT permanently attach to all 50 user tabs.** Attaching to tabs produces the Chrome developer infobar and consumes memory.
* **On-Demand Attachment:** When an action command (`click`, `type`, `press`, `scroll`) targets a tab:
  1. Check if `tabId` is currently attached in `attachedTabs: Set<number>`.
  2. If not attached, call:
     ```typescript
     await chrome.debugger.attach({ tabId }, '1.3');
     await chrome.debugger.sendCommand({ tabId }, 'Input.setIgnoreInputEvents', { ignore: false });
     attachedTabs.add(tabId);
     ```
  3. Execute the CDP input command.
  4. Retain attachment while the session/tab remains active.

### 3.2 Handling Detached Events Gracefully
Chrome automatically detaches the debugger under two conditions:
1. User clicks the "Cancel" button on the yellow infobar.
2. The tab navigates to a restricted scheme (e.g., `chrome://`, `chrome-extension://`).

The service worker listens for detachment and cleans up state:
```typescript
chrome.debugger.onDetach.addListener((source, reason) => {
  if (source.tabId) {
    attachedTabs.delete(source.tabId);
    // Broadcast event to daemon
    notifyDaemon({ event: 'debugger.detached', tabId: source.tabId, reason });
  }
});
```

---

## 4. Content Script Architecture & DOM Indexing

The content script runs in the context of the web page and handles:
1. Viewport geometry calculation.
2. Identification of interactive nodes.
3. Assignment of sequential element IDs (`[1]`, `[2]`, `[3]`).
4. Set-of-Mark (SoM) overlay badge rendering.

### 4.1 Index Map Caching & Stale Target Prevention
* Each time `page.snap` is requested, the content script increments a monotonic `snapshotId`.
* An in-memory array `indexedElements: ElementRecord[]` is populated:
  ```typescript
  interface ElementRecord {
    id: number;
    element: Element;
    rect: DOMRect;
    tag: string;
    label: string;
  }
  ```
* When an action command arrives targeting `id: 4`:
  * The content script looks up `indexedElements[4]`.
  * Verifies `element.isConnected === true` (is still attached to DOM).
  * Calls `element.getBoundingClientRect()` to get fresh coordinates (in case of dynamic reflow).
  * Returns fresh viewport center $(x, y)$ coordinates to the service worker to dispatch via `chrome.debugger`.

### 4.2 Dynamic Script Injection for Pre-Existing Tabs
When the extension is installed or reloaded, tabs that were already open do not have the content script running. The service worker dynamically injects the content script upon first interaction:

```typescript
async function ensureContentScriptInjected(tabId: number): Promise<void> {
  try {
    await chrome.tabs.sendMessage(tabId, { action: 'ping' });
  } catch {
    // Ping failed; inject script
    await chrome.scripting.executeScript({
      target: { tabId },
      files: ['src/content/indexer.js'],
    });
  }
}
```

---

## 5. Complete Manifest V3 Declaration (`manifest.json`)

```json
{
  "manifest_version": 3,
  "name": "chrome-ctl Bridge",
  "version": "1.0.0",
  "description": "High-efficiency, zero-detection automation bridge for autonomous AI agents.",
  "permissions": [
    "activeTab",
    "tabs",
    "debugger",
    "scripting",
    "storage",
    "offscreen"
  ],
  "host_permissions": [
    "<all_urls>"
  ],
  "background": {
    "service_worker": "src/background/service_worker.js",
    "type": "module"
  },
  "action": {
    "default_popup": "src/popup/popup.html",
    "default_icon": {
      "16": "icons/icon16.png",
      "48": "icons/icon48.png",
      "128": "icons/icon128.png"
    }
  },
  "content_scripts": [
    {
      "matches": ["<all_urls>"],
      "js": ["src/content/indexer.js"],
      "run_at": "document_idle"
    }
  ]
}
```
