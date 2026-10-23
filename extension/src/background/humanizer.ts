/**
 * Pure humanizer math for Phase 3 actions. No chrome.* access and no timers:
 * every delay is returned as data so the unit tests can run under plain Node.
 */

export interface Point2D {
  x: number;
  y: number;
}

export interface PathPoint {
  x: number;
  y: number;
  delayMs: number;
}

export interface MousePath {
  points: PathPoint[];
  durationMs: number;
}

export interface ClickRect {
  left: number;
  top: number;
  width: number;
  height: number;
  right?: number;
  bottom?: number;
}

export interface TypingStep {
  kind: "char" | "key";
  value: string;
  /** Pause after key-up (or after the character dispatch). */
  delayMs: number;
  /** Key-down to key-up hold time. */
  holdMs: number;
}

export interface TypingPlan {
  steps: TypingStep[];
  typosInjected: number;
}

export interface ScrollTick {
  delta: number;
  delayMs: number;
  /** Reading pause after this tick (0 when no pause is due). */
  pauseMs: number;
}

export interface ScrollPlan {
  ticks: ScrollTick[];
  totalPauseMs: number;
}

export interface KeyDefinition {
  key: string;
  code: string;
  windowsVirtualKeyCode: number;
  text?: string;
  shift?: boolean;
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

let gaussianSpare: number | undefined;

/** Standard Box-Muller transform. */
export function randomGaussian(mean: number, sd: number): number {
  if (gaussianSpare !== undefined) {
    const spare = gaussianSpare;
    gaussianSpare = undefined;
    return mean + sd * spare;
  }

  let u = 0;
  let v = 0;
  while (u === 0) {
    u = Math.random();
  }
  while (v === 0) {
    v = Math.random();
  }

  const magnitude = Math.sqrt(-2 * Math.log(u));
  gaussianSpare = magnitude * Math.sin(2 * Math.PI * v);
  return mean + sd * magnitude * Math.cos(2 * Math.PI * v);
}

/** Inclusive integer range. */
export function randomInt(min: number, max: number): number {
  const lo = Math.ceil(min);
  const hi = Math.floor(max);
  if (hi <= lo) {
    return lo;
  }
  return lo + Math.floor(Math.random() * (hi - lo + 1));
}

function sigmoid(tau: number): number {
  return 1 / (1 + Math.exp(-10 * (tau - 0.5)));
}

/**
 * Plan a cubic Bézier cursor path with Fitts's-law duration, sigmoid velocity
 * warping, and per-point perpendicular tremor.
 */
export function planMousePath(
  from: Point2D,
  to: Point2D,
  targetWidth: number,
): MousePath {
  const deltaX = to.x - from.x;
  const deltaY = to.y - from.y;
  const distance = Math.hypot(deltaX, deltaY);
  const count = clamp(Math.round(distance / 12), 8, 60);

  const rawDuration =
    120 +
    80 * Math.log2(1 + distance / Math.max(targetWidth, 10)) +
    randomGaussian(0, 25);
  const durationMs = Math.max(120, rawDuration);

  const theta = distance === 0 ? 0 : Math.atan2(deltaY, deltaX);
  const normalX = -Math.sin(theta);
  const normalY = Math.cos(theta);

  const curvature1 = randomGaussian(0, 0.25 * distance);
  const curvature2 = randomGaussian(0, 0.25 * distance);

  const control1X = from.x + deltaX / 3 + normalX * curvature1;
  const control1Y = from.y + deltaY / 3 + normalY * curvature1;
  const control2X = from.x + (2 * deltaX) / 3 + normalX * curvature2;
  const control2Y = from.y + (2 * deltaY) / 3 + normalY * curvature2;

  const s0 = sigmoid(0);
  const s1 = sigmoid(1);
  const span = s1 - s0;

  const points: PathPoint[] = [];
  let previousS = 0;

  for (let i = 0; i < count; i += 1) {
    const tau = count === 1 ? 1 : i / (count - 1);
    const eased = (sigmoid(tau) - s0) / span;
    const inverse = 1 - tau;

    const bezierX =
      inverse ** 3 * from.x +
      3 * inverse ** 2 * tau * control1X +
      3 * inverse * tau ** 2 * control2X +
      tau ** 3 * to.x;
    const bezierY =
      inverse ** 3 * from.y +
      3 * inverse ** 2 * tau * control1Y +
      3 * inverse * tau ** 2 * control2Y +
      tau ** 3 * to.y;

    // Keep the endpoints pinned; jitter the interior of the motion.
    const interior = distance > 0 && i > 0 && i < count - 1;
    const jitter = interior ? randomGaussian(0, 0.8) : 0;

    const delayMs = i === 0 ? 0 : Math.max(0, (eased - previousS) * durationMs);
    previousS = eased;

    points.push({
      x: bezierX + normalX * jitter,
      y: bezierY + normalY * jitter,
      delayMs,
    });
  }

  return { points, durationMs: Math.round(durationMs) };
}

/**
 * Gaussian click landing inside the element, inset 4px from each edge.
 * Elements smaller than 8px in either axis collapse to the center.
 */
export function clickLanding(rect: ClickRect): Point2D {
  const right = typeof rect.right === "number" ? rect.right : rect.left + rect.width;
  const bottom = typeof rect.bottom === "number" ? rect.bottom : rect.top + rect.height;
  const centerX = rect.left + rect.width / 2;
  const centerY = rect.top + rect.height / 2;

  if (rect.width < 8 || rect.height < 8) {
    return { x: centerX, y: centerY };
  }

  const x = centerX + randomGaussian(0, rect.width / 6);
  const y = centerY + randomGaussian(0, rect.height / 6);

  return {
    x: clamp(x, rect.left + 4, right - 4),
    y: clamp(y, rect.top + 4, bottom - 4),
  };
}

const COMMON_BIGRAMS = new Set(["th", "he", "in", "er", "an", "re", "on", "at"]);

const PUNCTUATION = new Set([".", ",", "!", "?", ":", ";"]);

const QWERTY_NEIGHBORS: Record<string, string[]> = {
  q: ["w", "a", "s"],
  w: ["q", "e", "a", "s", "d"],
  e: ["w", "r", "s", "d", "f"],
  r: ["e", "t", "d", "f", "g"],
  t: ["r", "y", "f", "g", "h"],
  y: ["t", "u", "g", "h", "j"],
  u: ["y", "i", "h", "j", "k"],
  i: ["u", "o", "j", "k", "l"],
  o: ["i", "p", "k", "l"],
  p: ["o", "l"],
  a: ["q", "w", "s", "z"],
  s: ["a", "w", "e", "d", "z", "x"],
  d: ["s", "e", "r", "f", "x", "c"],
  f: ["d", "r", "t", "g", "c", "v"],
  g: ["f", "t", "y", "h", "v", "b"],
  h: ["g", "y", "u", "j", "b", "n"],
  j: ["h", "u", "i", "k", "n", "m"],
  k: ["j", "i", "o", "l", "m"],
  l: ["k", "o", "p"],
  z: ["a", "s", "x"],
  x: ["z", "s", "d", "c"],
  c: ["x", "d", "f", "v"],
  v: ["c", "f", "g", "b"],
  b: ["v", "g", "h", "n"],
  n: ["b", "h", "j", "m"],
  m: ["n", "j", "k"],
  1: ["2", "q"],
  2: ["1", "3", "q", "w"],
  3: ["2", "4", "w", "e"],
  4: ["3", "5", "e", "r"],
  5: ["4", "6", "r", "t"],
  6: ["5", "7", "t", "y"],
  7: ["6", "8", "y", "u"],
  8: ["7", "9", "u", "i"],
  9: ["8", "0", "i", "o"],
  0: ["9", "o", "p"],
};

function flightTimeMs(): number {
  return Math.exp(randomGaussian(4.2, 0.3));
}

function adjacentKey(ch: string): string | null {
  const lower = ch.toLowerCase();
  const neighbors = QWERTY_NEIGHBORS[lower];
  if (!neighbors || neighbors.length === 0) {
    return null;
  }
  return neighbors[randomInt(0, neighbors.length - 1)] ?? null;
}

/**
 * Plan cadence typing. Each character emits one step (the down/hold/up cycle is
 * described by `holdMs` + `delayMs`), with optional typo/backspace bursts.
 */
export function planTyping(text: string, typoRate: number): TypingPlan {
  const rate = Number.isFinite(typoRate) ? clamp(typoRate, 0, 1) : 0;
  const steps: TypingStep[] = [];
  let typosInjected = 0;
  let previous = "";

  for (const ch of text) {
    if (ch === "\n") {
      steps.push({
        kind: "key",
        value: "Enter",
        delayMs: Math.round(flightTimeMs()),
        holdMs: randomInt(45, 85),
      });
      previous = "\n";
      continue;
    }

    let delay = flightTimeMs();
    if (COMMON_BIGRAMS.has(`${previous}${ch}`.toLowerCase())) {
      delay *= 0.65;
    }
    if (ch === " ") {
      delay += randomInt(120, 280);
    } else if (PUNCTUATION.has(ch)) {
      delay += randomInt(250, 550);
    }

    if (rate > 0 && Math.random() < rate) {
      const wrong = adjacentKey(ch);
      if (wrong !== null) {
        typosInjected += 1;
        steps.push({
          kind: "char",
          value: wrong,
          delayMs: randomInt(140, 260),
          holdMs: randomInt(45, 85),
        });
        steps.push({
          kind: "key",
          value: "Backspace",
          delayMs: randomInt(90, 180),
          holdMs: randomInt(45, 85),
        });
      }
    }

    steps.push({
      kind: "char",
      value: ch,
      delayMs: Math.max(0, Math.round(delay)),
      holdMs: randomInt(45, 85),
    });
    previous = ch;
  }

  return { steps, typosInjected };
}

/** Alias kept for callers that use the specification's original name. */
export const planKeystrokes = planTyping;

/**
 * Momentum wheel ticks: exponentially decaying deltas summing exactly to
 * `distancePx` (signed), plus reading pauses every 600-900px.
 */
export function planScroll(distancePx: number): ScrollPlan {
  const tickCount = clamp(Math.round(Math.abs(distancePx) / 100), 3, 40);
  const lambda = 0.88;

  const weights: number[] = [];
  let weightSum = 0;
  for (let i = 0; i < tickCount; i += 1) {
    const weight = lambda ** i;
    weights.push(weight);
    weightSum += weight;
  }

  const ticks: ScrollTick[] = [];
  let assigned = 0;
  let sincePause = 0;
  let nextPauseAt = randomInt(600, 900);
  let totalPauseMs = 0;

  for (let i = 0; i < tickCount; i += 1) {
    const isLast = i === tickCount - 1;
    const delta = isLast
      ? distancePx - assigned
      : Math.round((distancePx * weights[i]!) / weightSum);
    assigned += delta;

    let pauseMs = 0;
    sincePause += Math.abs(delta);
    if (sincePause >= nextPauseAt) {
      pauseMs = randomInt(1200, 3500);
      totalPauseMs += pauseMs;
      sincePause = 0;
      nextPauseAt = randomInt(600, 900);
    }

    ticks.push({ delta, delayMs: randomInt(16, 32), pauseMs });
  }

  return { ticks, totalPauseMs };
}

/** CDP modifier bitmask. Unknown names are ignored. */
export function modifiersMask(names: string[]): number {
  let mask = 0;
  for (const name of names) {
    switch (name.toLowerCase()) {
      case "alt":
        mask |= 1;
        break;
      case "ctrl":
      case "control":
        mask |= 2;
        break;
      case "meta":
      case "cmd":
      case "command":
        mask |= 4;
        break;
      case "shift":
        mask |= 8;
        break;
      default:
        break;
    }
  }
  return mask;
}

interface SymbolKey {
  code: string;
  vk: number;
  shift: boolean;
}

const SYMBOL_KEYS: Record<string, SymbolKey> = {
  "-": { code: "Minus", vk: 189, shift: false },
  "=": { code: "Equal", vk: 187, shift: false },
  "[": { code: "BracketLeft", vk: 219, shift: false },
  "]": { code: "BracketRight", vk: 221, shift: false },
  "\\": { code: "Backslash", vk: 220, shift: false },
  ";": { code: "Semicolon", vk: 186, shift: false },
  "'": { code: "Quote", vk: 222, shift: false },
  ",": { code: "Comma", vk: 188, shift: false },
  ".": { code: "Period", vk: 190, shift: false },
  "/": { code: "Slash", vk: 191, shift: false },
  "`": { code: "Backquote", vk: 192, shift: false },
  "!": { code: "Digit1", vk: 49, shift: true },
  "@": { code: "Digit2", vk: 50, shift: true },
  "#": { code: "Digit3", vk: 51, shift: true },
  $: { code: "Digit4", vk: 52, shift: true },
  "%": { code: "Digit5", vk: 53, shift: true },
  "^": { code: "Digit6", vk: 54, shift: true },
  "&": { code: "Digit7", vk: 55, shift: true },
  "*": { code: "Digit8", vk: 56, shift: true },
  "(": { code: "Digit9", vk: 57, shift: true },
  ")": { code: "Digit0", vk: 48, shift: true },
  _: { code: "Minus", vk: 189, shift: true },
  "+": { code: "Equal", vk: 187, shift: true },
  "{": { code: "BracketLeft", vk: 219, shift: true },
  "}": { code: "BracketRight", vk: 221, shift: true },
  "|": { code: "Backslash", vk: 220, shift: true },
  ":": { code: "Semicolon", vk: 186, shift: true },
  '"': { code: "Quote", vk: 222, shift: true },
  "<": { code: "Comma", vk: 188, shift: true },
  ">": { code: "Period", vk: 190, shift: true },
  "?": { code: "Slash", vk: 191, shift: true },
  "~": { code: "Backquote", vk: 192, shift: true },
};

const NAMED_KEYS: Record<string, KeyDefinition> = {
  enter: { key: "Enter", code: "Enter", windowsVirtualKeyCode: 13 },
  tab: { key: "Tab", code: "Tab", windowsVirtualKeyCode: 9 },
  escape: { key: "Escape", code: "Escape", windowsVirtualKeyCode: 27 },
  backspace: { key: "Backspace", code: "Backspace", windowsVirtualKeyCode: 8 },
  delete: { key: "Delete", code: "Delete", windowsVirtualKeyCode: 46 },
  space: { key: " ", code: "Space", windowsVirtualKeyCode: 32, text: " " },
  home: { key: "Home", code: "Home", windowsVirtualKeyCode: 36 },
  end: { key: "End", code: "End", windowsVirtualKeyCode: 35 },
  pageup: { key: "PageUp", code: "PageUp", windowsVirtualKeyCode: 33 },
  pagedown: { key: "PageDown", code: "PageDown", windowsVirtualKeyCode: 34 },
  arrowup: { key: "ArrowUp", code: "ArrowUp", windowsVirtualKeyCode: 38 },
  arrowdown: { key: "ArrowDown", code: "ArrowDown", windowsVirtualKeyCode: 40 },
  arrowleft: { key: "ArrowLeft", code: "ArrowLeft", windowsVirtualKeyCode: 37 },
  arrowright: { key: "ArrowRight", code: "ArrowRight", windowsVirtualKeyCode: 39 },
};

/** CDP key metadata for named keys and single printable characters. */
export function keyDefinition(key: string): KeyDefinition {
  const named = NAMED_KEYS[key.toLowerCase()];
  if (named) {
    return { ...named };
  }

  if (key === "\n" || key === "\r") {
    return { ...NAMED_KEYS.enter! };
  }

  if (key === " ") {
    return { ...NAMED_KEYS.space! };
  }

  if (key.length !== 1) {
    throw new Error(`unknown key: ${key}`);
  }

  if (/[a-z]/i.test(key)) {
    const upper = key.toUpperCase();
    return {
      key,
      code: `Key${upper}`,
      windowsVirtualKeyCode: upper.charCodeAt(0),
      text: key,
      shift: key !== key.toLowerCase(),
    };
  }

  if (/[0-9]/.test(key)) {
    return {
      key,
      code: `Digit${key}`,
      windowsVirtualKeyCode: key.charCodeAt(0),
      text: key,
    };
  }

  const symbol = SYMBOL_KEYS[key];
  if (symbol) {
    return {
      key,
      code: symbol.code,
      windowsVirtualKeyCode: symbol.vk,
      text: key,
      shift: symbol.shift,
    };
  }

  throw new Error(`unknown key: ${key}`);
}
