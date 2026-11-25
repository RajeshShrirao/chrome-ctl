import type { OverlayState } from "../shared/content.ts";

const HOST_ATTR = "data-chrome-ctl-overlay";

const STYLES = `
:host {
  all: initial;
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 2147483647;
}
.frame {
  position: fixed;
  inset: 0;
  pointer-events: none;
  box-sizing: border-box;
  transition: box-shadow 180ms ease;
}
.frame.idle {
  box-shadow: inset 0 0 0 3px rgba(76, 141, 255, 0.55);
}
.frame.running {
  box-shadow: inset 0 0 0 3px rgba(63, 185, 80, 0.9), inset 0 0 14px rgba(63, 185, 80, 0.35);
  animation: chromeCtlPulse 1.4s ease-in-out infinite;
}
.frame.paused {
  box-shadow: inset 0 0 0 3px rgba(210, 153, 34, 0.9), inset 0 0 12px rgba(210, 153, 34, 0.25);
}
@keyframes chromeCtlPulse {
  0%, 100% {
    box-shadow: inset 0 0 0 3px rgba(63, 185, 80, 0.85), inset 0 0 10px rgba(63, 185, 80, 0.25);
  }
  50% {
    box-shadow: inset 0 0 0 4px rgba(76, 141, 255, 0.95), inset 0 0 18px rgba(76, 141, 255, 0.4);
  }
}
`;

let hostEl: HTMLElement | null = null;
let frameEl: HTMLDivElement | null = null;

function removeHost(doc: Document): void {
  if (hostEl && hostEl.isConnected) {
    hostEl.remove();
  }
  const existing = doc.documentElement?.querySelector(`[${HOST_ATTR}]`);
  if (existing) {
    existing.remove();
  }
  hostEl = null;
  frameEl = null;
}

function ensureHost(doc: Document): HTMLDivElement | null {
  const root = doc.documentElement;
  if (!root) {
    return null;
  }
  if (hostEl && hostEl.isConnected && frameEl) {
    return frameEl;
  }
  removeHost(doc);

  const host = doc.createElement("div");
  host.setAttribute(HOST_ATTR, "1");
  host.style.cssText =
    "position:fixed;inset:0;pointer-events:none;z-index:2147483647;background:transparent;";

  const shadow = host.attachShadow({ mode: "closed" });
  const style = doc.createElement("style");
  style.textContent = STYLES;
  const frame = doc.createElement("div");
  frame.className = "frame";
  shadow.append(style, frame);
  root.appendChild(host);

  hostEl = host;
  frameEl = frame;
  return frame;
}

/**
 * Render or remove the subtle automation-window border glow.
 * Uses a closed Shadow DOM so page styles/scripts cannot interfere.
 */
export function setAutomationOverlay(state: OverlayState, doc: Document = document): void {
  if (state === "off") {
    removeHost(doc);
    return;
  }
  const frame = ensureHost(doc);
  if (!frame) {
    return;
  }
  frame.className = `frame ${state}`;
}
