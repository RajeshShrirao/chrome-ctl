/**
 * Pure humanizer math for Phase 3 actions. No chrome.* access and no timers:
 * every delay is returned as data so the unit tests can run under plain Node.
 *
 * Determinism: every planner takes an optional `Rng`. When an action starts it
 * creates one with `createRng(seed)` (crypto-seeded when no seed is supplied),
 * passes it to the planners, and reports `rng.seed()` in the result so the same
 * motion can be replayed exactly.
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
  /** Seed that reproduces this plan. */
  seed: string;
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
  kind: "char" | "key" | "insert";
  value: string;
  /** Pause after key-up (or after the character dispatch). */
  delayMs: number;
  /** Key-down to key-up hold time. */
  holdMs: number;
}

export interface TypingPlan {
  steps: TypingStep[];
  typosInjected: number;
  /** Seed that reproduces this plan. */
  seed: string;
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
  /** Seed that reproduces this plan. */
  seed: string;
}

export interface KeyDefinition {
  key: string;
  code: string;
  windowsVirtualKeyCode: number;
  text?: string;
  shift?: boolean;
}

/** Injectable randomness so every action can be replayed from a seed. */
export interface Rng {
  /** Uniform sample in [0, 1). */
  next(): number;
  /** Standard normal sample (mean 0, sd 1). */
  gaussian(): number;
  /** Hex seed that reproduces this stream from the start. */
  seed(): string;
}

type State = [number, number, number, number];

const TWO_PI = Math.PI * 2;

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

function rotl(value: number, bits: number): number {
  return ((value << bits) | (value >>> (32 - bits))) >>> 0;
}

function stateToSeed(state: State): string {
  return state.map((word) => (word >>> 0).toString(16).padStart(8, "0")).join("");
}

/** FNV-1a, used to turn arbitrary seed strings into xoshiro state words. */
function fnv1a(text: string): number {
  let hash = 0x811c9dc5;
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash >>> 0;
}

function cryptoState(): State {
  const words = new Uint32Array(4);
  const cryptoObject = (
    globalThis as { crypto?: { getRandomValues?: (array: Uint32Array) => Uint32Array } }
  ).crypto;
  if (cryptoObject && typeof cryptoObject.getRandomValues === "function") {
    cryptoObject.getRandomValues(words);
  } else {
    for (let index = 0; index < words.length; index += 1) {
      words[index] = Math.floor(Math.random() * 0x100000000);
    }
  }
  if ((words[0]! | words[1]! | words[2]! | words[3]!) === 0) {
    words[0] = 1;
  }
  return [words[0]!, words[1]!, words[2]!, words[3]!];
}

function stateFromSeed(seed: string): State {
  const hex = seed.toLowerCase().replace(/[^0-9a-f]/g, "");
  const state: State = [0, 0, 0, 0];
  for (let index = 0; index < 4; index += 1) {
    const chunk = hex.slice(index * 8, index * 8 + 8);
    state[index] = chunk.length === 8 ? parseInt(chunk, 16) >>> 0 : 0;
  }
  if ((state[0] | state[1] | state[2] | state[3]) === 0) {
    const hash = fnv1a(seed);
    state[0] = hash || 1;
    state[1] = (hash ^ 0x9e3779b9) >>> 0;
    state[2] = 0x243f6a88;
    state[3] = 0xb7e15162;
  }
  return state;
}

function rngFromState(state: State, seed: string): Rng {
  let [a, b, c, d] = state;
  let spare: number | undefined;

  function nextUint32(): number {
    const result = Math.imul(rotl(Math.imul(b, 5) >>> 0, 7), 9) >>> 0;
    const t = (b << 9) >>> 0;
    c ^= a;
    d ^= b;
    b ^= c;
    a ^= d;
    c ^= t;
    d = rotl(d, 11);
    return result;
  }

  function next(): number {
    return nextUint32() / 0x100000000;
  }

  function gaussian(): number {
    if (spare !== undefined) {
      const value = spare;
      spare = undefined;
      return value;
    }
    let u = 0;
    let v = 0;
    while (u === 0) {
      u = next();
    }
    while (v === 0) {
      v = next();
    }
    const magnitude = Math.sqrt(-2 * Math.log(u));
    spare = magnitude * Math.sin(TWO_PI * v);
    return magnitude * Math.cos(TWO_PI * v);
  }

  return { next, gaussian, seed: () => seed };
}

/**
 * Create an xoshiro128** stream. A hex seed restores an exact stream; any other
 * string is hashed into state so callers can use human-readable seeds.
 */
export function createRng(seed?: string): Rng {
  if (seed !== undefined && seed !== "") {
    return rngFromState(stateFromSeed(seed), seed);
  }
  const state = cryptoState();
  return rngFromState(state, stateToSeed(state));
}

const defaultRng = createRng();

/** Standard Box-Muller transform. */
export function randomGaussian(mean: number, sd: number, rng: Rng = defaultRng): number {
  return mean + sd * rng.gaussian();
}

/** Inclusive integer range. */
export function randomInt(min: number, max: number, rng: Rng = defaultRng): number {
  const lo = Math.ceil(min);
  const hi = Math.floor(max);
  if (hi <= lo) {
    return lo;
  }
  return lo + Math.floor(rng.next() * (hi - lo + 1));
}

/**
 * Minimum-jerk position profile: zero velocity and acceleration at both ends,
 * which reads as a natural reaching motion.
 */
export function minimumJerk(tau: number): number {
  const clamped = clamp(tau, 0, 1);
  return clamped ** 3 * (10 - 15 * clamped + 6 * clamped ** 2);
}

/** Discrete Ornstein-Uhlenbeck tremor: mean-reverting, not white noise. */
class Tremor {
  private value = 0;
  private readonly theta: number;
  private readonly sigma: number;

  constructor(theta = 0.15, sigma = 1.0) {
    this.theta = theta;
    this.sigma = sigma;
  }

  next(dt: number, rng: Rng): number {
    const step = Math.max(dt, 1e-4);
    this.value += -this.theta * this.value * step + this.sigma * Math.sqrt(step) * rng.gaussian();
    return this.value;
  }
}

/**
 * Plan a cubic Bézier cursor path with Fitts's-law duration, a minimum-jerk
 * velocity profile, OU tremor, and a small overshoot/correction for far targets.
 */
export function planMousePath(
  from: Point2D,
  to: Point2D,
  targetWidth: number,
  rng: Rng = defaultRng,
): MousePath {
  const deltaX = to.x - from.x;
  const deltaY = to.y - from.y;
  const distance = Math.hypot(deltaX, deltaY);
  const count = clamp(Math.round(distance / 12), 8, 60);

  const rawDuration =
    120 +
    80 * Math.log2(1 + distance / Math.max(targetWidth, 10)) +
    randomGaussian(0, 25, rng);
  const durationMs = Math.max(120, rawDuration);

  const theta = distance === 0 ? 0 : Math.atan2(deltaY, deltaX);
  const normalX = -Math.sin(theta);
  const normalY = Math.cos(theta);
  const unitX = distance === 0 ? 0 : deltaX / distance;
  const unitY = distance === 0 ? 0 : deltaY / distance;

  const curvature1 = randomGaussian(0, 0.25 * distance, rng);
  const curvature2 = randomGaussian(0, 0.25 * distance, rng);

  const control1X = from.x + deltaX / 3 + normalX * curvature1;
  const control1Y = from.y + deltaY / 3 + normalY * curvature1;
  const control2X = from.x + (2 * deltaX) / 3 + normalX * curvature2;
  const control2Y = from.y + (2 * deltaY) / 3 + normalY * curvature2;

  const overshoot = distance > 350 ? Math.min(18, distance * 0.02) : 0;
  const overshootAt = (tau: number): number => {
    if (overshoot === 0 || tau <= 0.78 || tau >= 1) {
      return 0;
    }
    return overshoot * Math.sin(Math.PI * ((tau - 0.78) / 0.22));
  };

  const tremor = new Tremor();
  const points: PathPoint[] = [];
  let previousS = 0;

  for (let index = 0; index < count; index += 1) {
    const tau = count === 1 ? 1 : index / (count - 1);
    const eased = minimumJerk(tau);
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

    // Keep the endpoints pinned; jitter and overshoot only the interior motion.
    const interior = distance > 0 && index > 0 && index < count - 1;
    const jitter = interior ? tremor.next(1 / count, rng) : 0;
    const overshootOffset = overshootAt(tau);

    const delayMs = index === 0 ? 0 : Math.max(0, (eased - previousS) * durationMs);
    previousS = eased;

    points.push({
      x: bezierX + normalX * jitter + unitX * overshootOffset,
      y: bezierY + normalY * jitter + unitY * overshootOffset,
      delayMs,
    });
  }

  return { points, durationMs: Math.round(durationMs), seed: rng.seed() };
}

/**
 * Gaussian click landing inside the element, inset 4px from each edge.
 * Elements smaller than 8px in either axis collapse to the center.
 */
export function clickLanding(rect: ClickRect, rng: Rng = defaultRng): Point2D {
  const right = typeof rect.right === "number" ? rect.right : rect.left + rect.width;
  const bottom = typeof rect.bottom === "number" ? rect.bottom : rect.top + rect.height;
  const centerX = rect.left + rect.width / 2;
  const centerY = rect.top + rect.height / 2;

  if (rect.width < 8 || rect.height < 8) {
    return { x: centerX, y: centerY };
  }

  const x = centerX + randomGaussian(0, rect.width / 6, rng);
  const y = centerY + randomGaussian(0, rect.height / 6, rng);

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

function flightTimeMs(rng: Rng): number {
  // Log-normal flight times: mostly ~65ms, with an occasional slow key.
  return Math.exp(randomGaussian(4.2, 0.3, rng));
}

function adjacentKey(ch: string, rng: Rng): string | null {
  const lower = ch.toLowerCase();
  const neighbors = QWERTY_NEIGHBORS[lower];
  if (!neighbors || neighbors.length === 0) {
    return null;
  }
  return neighbors[randomInt(0, neighbors.length - 1, rng)] ?? null;
}

type SegmenterLike = {
  segment(input: string): Iterable<{ segment: string }>;
};

function graphemes(text: string): string[] {
  const SegmenterCtor = (
    Intl as unknown as { Segmenter?: new (locales?: string, options?: { granularity: string }) => SegmenterLike }
  ).Segmenter;
  if (SegmenterCtor) {
    const segmenter = new SegmenterCtor(undefined, { granularity: "grapheme" });
    return Array.from(segmenter.segment(text), (part) => part.segment);
  }
  return Array.from(text);
}

function isTypeable(grapheme: string): boolean {
  try {
    keyDefinition(grapheme);
    return true;
  } catch {
    return false;
  }
}

/**
 * Plan cadence typing. Each grapheme emits one step (the down/hold/up cycle is
 * described by `holdMs` + `delayMs`), with optional typo/backspace bursts.
 * Graphemes with no key code are emitted as `insert` steps so the executor can
 * use `Input.insertText`.
 */
export function planTyping(text: string, typoRate: number, rng: Rng = defaultRng): TypingPlan {
  const rate = Number.isFinite(typoRate) ? clamp(typoRate, 0, 1) : 0;
  const steps: TypingStep[] = [];
  let typosInjected = 0;
  let previous = "";

  for (const ch of graphemes(text)) {
    if (ch === "\n" || ch === "\r\n") {
      steps.push({
        kind: "key",
        value: "Enter",
        delayMs: Math.round(flightTimeMs(rng)),
        holdMs: randomInt(45, 85, rng),
      });
      previous = "\n";
      continue;
    }

    let delay = flightTimeMs(rng);
    if (COMMON_BIGRAMS.has(`${previous}${ch}`.toLowerCase())) {
      delay *= 0.65;
    }
    if (ch === " ") {
      delay += randomInt(120, 280, rng);
    } else if (PUNCTUATION.has(ch)) {
      delay += randomInt(250, 550, rng);
    }

    if (rate > 0 && rng.next() < rate && isTypeable(ch)) {
      const wrong = adjacentKey(ch, rng);
      if (wrong !== null) {
        typosInjected += 1;
        steps.push({
          kind: "char",
          value: wrong,
          delayMs: randomInt(140, 260, rng),
          holdMs: randomInt(45, 85, rng),
        });
        steps.push({
          kind: "key",
          value: "Backspace",
          delayMs: randomInt(90, 180, rng),
          holdMs: randomInt(45, 85, rng),
        });
      }
    }

    steps.push({
      kind: isTypeable(ch) ? "char" : "insert",
      value: ch,
      delayMs: Math.max(0, Math.round(delay)),
      holdMs: randomInt(45, 85, rng),
    });
    previous = ch;
  }

  return { steps, typosInjected, seed: rng.seed() };
}

/** Alias kept for callers that use the specification's original name. */
export const planKeystrokes = planTyping;

/**
 * Momentum wheel ticks: exponentially decaying deltas summing exactly to
 * `distancePx` (signed), plus reading pauses every 600-900px.
 */
export function planScroll(distancePx: number, rng: Rng = defaultRng): ScrollPlan {
  const tickCount = clamp(Math.round(Math.abs(distancePx) / 100), 3, 40);
  const lambda = 0.88;

  const weights: number[] = [];
  let weightSum = 0;
  for (let index = 0; index < tickCount; index += 1) {
    const weight = lambda ** index;
    weights.push(weight);
    weightSum += weight;
  }

  const ticks: ScrollTick[] = [];
  let assigned = 0;
  let sincePause = 0;
  let nextPauseAt = randomInt(600, 900, rng);
  let totalPauseMs = 0;

  for (let index = 0; index < tickCount; index += 1) {
    const isLast = index === tickCount - 1;
    const delta = isLast
      ? distancePx - assigned
      : Math.round((distancePx * weights[index]!) / weightSum);
    assigned += delta;

    let pauseMs = 0;
    sincePause += Math.abs(delta);
    if (sincePause >= nextPauseAt) {
      pauseMs = randomInt(1200, 3500, rng);
      totalPauseMs += pauseMs;
      sincePause = 0;
      nextPauseAt = randomInt(600, 900, rng);
    }

    ticks.push({ delta, delayMs: randomInt(16, 32, rng), pauseMs });
  }

  return { ticks, totalPauseMs, seed: rng.seed() };
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
