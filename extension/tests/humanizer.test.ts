import test from "node:test";
import assert from "node:assert/strict";

import {
  clickLanding,
  createRng,
  keyDefinition,
  minimumJerk,
  modifiersMask,
  planMousePath,
  planScroll,
  planTyping,
  randomGaussian,
  randomInt,
} from "../src/background/humanizer.ts";

test("randomGaussian is finite and roughly centred", () => {
  let sum = 0;
  const samples = 4000;
  for (let i = 0; i < samples; i += 1) {
    const value = randomGaussian(10, 2);
    assert.ok(Number.isFinite(value));
    sum += value;
  }
  assert.ok(Math.abs(sum / samples - 10) < 0.25);
});

test("randomInt is inclusive on both ends", () => {
  const seen = new Set<number>();
  for (let i = 0; i < 500; i += 1) {
    const value = randomInt(1, 4);
    assert.ok(value >= 1 && value <= 4);
    seen.add(value);
  }
  assert.deepEqual([...seen].sort(), [1, 2, 3, 4]);
  assert.equal(randomInt(7, 7), 7);
});

test("mouse paths start at from, end at to, and carry a sane duration", () => {
  const from = { x: 120, y: 90 };
  const to = { x: 720, y: 530 };

  for (let run = 0; run < 40; run += 1) {
    const path = planMousePath(from, to, 60);

    assert.ok(path.points.length >= 8 && path.points.length <= 60);
    const first = path.points[0]!;
    const last = path.points[path.points.length - 1]!;
    assert.ok(Math.hypot(first.x - from.x, first.y - from.y) < 1e-6);
    assert.ok(Math.hypot(last.x - to.x, last.y - to.y) < 1e-6);

    let delaySum = 0;
    for (const point of path.points) {
      assert.ok(Number.isFinite(point.x) && Number.isFinite(point.y));
      assert.ok(point.delayMs >= 0);
      delaySum += point.delayMs;
    }

    assert.ok(path.durationMs >= 120);
    assert.ok(path.durationMs <= 1200);
    assert.ok(Math.abs(delaySum - path.durationMs) < 1);
  }
});

test("mouse path handles a zero-distance move", () => {
  const path = planMousePath({ x: 50, y: 50 }, { x: 50, y: 50 }, 30);
  assert.ok(path.points.length >= 8);
  assert.ok(path.durationMs >= 120);
  for (const point of path.points) {
    assert.ok(Math.hypot(point.x - 50, point.y - 50) < 1);
  }
});

test("click landing stays inside the 4px inset across 200 samples", () => {
  const rect = { left: 50, top: 20, width: 100, height: 40 };
  for (let run = 0; run < 200; run += 1) {
    const landing = clickLanding(rect);
    assert.ok(landing.x >= 54 && landing.x <= 146, `x=${landing.x}`);
    assert.ok(landing.y >= 24 && landing.y <= 56, `y=${landing.y}`);
  }
});

test("click landing collapses to the center for tiny targets", () => {
  const small = clickLanding({ left: 10, top: 10, width: 6, height: 4 });
  assert.deepEqual(small, { x: 13, y: 12 });
});

test("planTyping emits ordered character steps with punctuation and space pauses", () => {
  const plan = planTyping("hi there, ok!", 0);
  assert.equal(plan.typosInjected, 0);

  assert.deepEqual(
    plan.steps.map((step) => step.value).join(""),
    "hi there, ok!",
  );
  for (const step of plan.steps) {
    assert.ok(step.delayMs >= 0);
    assert.ok(step.holdMs >= 45 && step.holdMs <= 85);
  }

  const space = plan.steps.find((step) => step.value === " ");
  assert.ok(space);
  assert.ok(space.delayMs >= 120);

  const comma = plan.steps.find((step) => step.value === ",");
  assert.ok(comma);
  assert.ok(comma.delayMs >= 250);

  const bang = plan.steps.find((step) => step.value === "!");
  assert.ok(bang);
  assert.ok(bang.delayMs >= 250);
});

test("planTyping accelerates common bigrams", () => {
  const sample = (text: string): number => {
    let total = 0;
    for (let i = 0; i < 600; i += 1) {
      total += planTyping(text, 0).steps[1]!.delayMs;
    }
    return total / 600;
  };

  const bigram = sample("th");
  const control = sample("qz");
  assert.ok(bigram < control * 0.85, `bigram=${bigram} control=${control}`);
});

test("planTyping maps newlines to Enter", () => {
  const plan = planTyping("a\nb", 0);
  const enter = plan.steps.find((step) => step.value === "Enter");
  assert.ok(enter);
  assert.equal(enter.kind, "key");
  assert.equal(plan.steps.filter((step) => step.value === "Enter").length, 1);
});

test("planTyping injects a typo + backspace for letters and digits at typoRate 1", () => {
  const plan = planTyping("hi 5", 1);

  assert.equal(plan.typosInjected, 3);
  const backspaces = plan.steps.filter((step) => step.value === "Backspace");
  assert.equal(backspaces.length, 3);
  for (const backspace of backspaces) {
    assert.equal(backspace.kind, "key");
    assert.ok(backspace.delayMs >= 90 && backspace.delayMs <= 180);
  }

  // Every injected typo is followed by backspace, then the intended char.
  for (let i = 0; i < plan.steps.length - 2; i += 1) {
    if (plan.steps[i]!.value === "Backspace") {
      const intended = plan.steps[i + 1]!;
      assert.equal(intended.kind, "char");
    }
  }

  const none = planTyping("hi 5", 0);
  assert.equal(none.typosInjected, 0);
  assert.equal(none.steps.filter((step) => step.value === "Backspace").length, 0);
});

test("planTyping never injects typos for spaces or punctuation", () => {
  const plan = planTyping(" .,;", 1);
  assert.equal(plan.typosInjected, 0);
});

test("planScroll deltas sum exactly to the requested distance", () => {
  for (const distance of [0, 50, 250, 1200, 2000, -1500]) {
    const plan = planScroll(distance);
    assert.ok(plan.ticks.length >= 3 && plan.ticks.length <= 40);

    const sum = plan.ticks.reduce((total, tick) => total + tick.delta, 0);
    assert.equal(sum, distance);

    let pauses = 0;
    for (const tick of plan.ticks) {
      assert.ok(tick.delayMs >= 16 && tick.delayMs <= 32);
      if (tick.pauseMs > 0) {
        assert.ok(tick.pauseMs >= 1200 && tick.pauseMs <= 3500);
        pauses += tick.pauseMs;
      }
    }
    assert.equal(plan.totalPauseMs, pauses);
  }
});

test("planScroll inserts reading pauses on long distances only", () => {
  const short = planScroll(300);
  assert.equal(short.totalPauseMs, 0);

  const long = planScroll(2600);
  assert.ok(long.totalPauseMs >= 1200);
  assert.ok(long.ticks.some((tick) => tick.pauseMs > 0));
});

test("modifiersMask maps names case-insensitively and ignores unknowns", () => {
  assert.equal(modifiersMask([]), 0);
  assert.equal(modifiersMask(["Alt"]), 1);
  assert.equal(modifiersMask(["Ctrl"]), 2);
  assert.equal(modifiersMask(["Control"]), 2);
  assert.equal(modifiersMask(["Meta"]), 4);
  assert.equal(modifiersMask(["Cmd"]), 4);
  assert.equal(modifiersMask(["Command"]), 4);
  assert.equal(modifiersMask(["Shift"]), 8);
  assert.equal(modifiersMask(["ctrl", "SHIFT"]), 10);
  assert.equal(modifiersMask(["Alt", "Meta", "Command"]), 5);
  assert.equal(modifiersMask(["Hyper"]), 0);
});

test("keyDefinition covers every named key", () => {
  const named: Array<[string, string, number]> = [
    ["Enter", "Enter", 13],
    ["Tab", "Tab", 9],
    ["Escape", "Escape", 27],
    ["Backspace", "Backspace", 8],
    ["Delete", "Delete", 46],
    ["Space", "Space", 32],
    ["Home", "Home", 36],
    ["End", "End", 35],
    ["PageUp", "PageUp", 33],
    ["PageDown", "PageDown", 34],
    ["ArrowUp", "ArrowUp", 38],
    ["ArrowDown", "ArrowDown", 40],
    ["ArrowLeft", "ArrowLeft", 37],
    ["ArrowRight", "ArrowRight", 39],
  ];

  for (const [name, code, vk] of named) {
    const definition = keyDefinition(name);
    assert.equal(definition.key, name.toLowerCase() === "space" ? " " : name);
    assert.equal(definition.code, code);
    assert.equal(definition.windowsVirtualKeyCode, vk);
  }
  assert.equal(keyDefinition("space").text, " ");
});

test("keyDefinition handles printable characters and shifted symbols", () => {
  const a = keyDefinition("a");
  assert.deepEqual(a, {
    key: "a",
    code: "KeyA",
    windowsVirtualKeyCode: 65,
    text: "a",
    shift: false,
  });

  const upper = keyDefinition("A");
  assert.equal(upper.code, "KeyA");
  assert.equal(upper.shift, true);
  assert.equal(upper.text, "A");

  const digit = keyDefinition("1");
  assert.equal(digit.code, "Digit1");
  assert.equal(digit.windowsVirtualKeyCode, 49);
  assert.equal(digit.shift, undefined);

  const bang = keyDefinition("!");
  assert.equal(bang.code, "Digit1");
  assert.equal(bang.shift, true);
  assert.equal(bang.text, "!");

  const percent = keyDefinition("%");
  assert.equal(percent.code, "Digit5");
  assert.equal(percent.shift, true);
  assert.equal(percent.text, "%");
});

test("keyDefinition rejects unknown multi-character keys", () => {
  assert.throws(() => keyDefinition("F13"), /unknown key: F13/);
  assert.throws(() => keyDefinition("Hyper"), /unknown key: Hyper/);
  assert.throws(() => keyDefinition(""), /unknown key/);
});

test("minimumJerk is a monotone 0-to-1 profile with zero end velocity", () => {
  assert.equal(minimumJerk(0), 0);
  assert.equal(minimumJerk(1), 1);
  assert.ok(Math.abs(minimumJerk(0.5) - 0.5) < 1e-9);
  let previous = -Infinity;
  for (let tau = 0; tau <= 1.0001; tau += 0.01) {
    const value = minimumJerk(tau);
    assert.ok(value >= previous - 1e-9, `not monotone at tau=${tau}`);
    previous = value;
  }
});

test("createRng is deterministic per seed and differs across seeds", () => {
  const draw = (seed: string): number[] => {
    const rng = createRng(seed);
    return Array.from({ length: 8 }, () => rng.gaussian());
  };
  assert.deepEqual(draw("replay-me"), draw("replay-me"));
  assert.notDeepEqual(draw("replay-me"), draw("other-seed"));

  const rng = createRng("abcdef0123456789abcdef0123456789");
  assert.equal(rng.seed(), "abcdef0123456789abcdef0123456789");
});

test("planners return the seed that reproduces them", () => {
  const from = { x: 10, y: 20 };
  const to = { x: 400, y: 300 };

  const pathA = planMousePath(from, to, 50, createRng("path-seed"));
  const pathB = planMousePath(from, to, 50, createRng("path-seed"));
  assert.equal(pathA.seed, "path-seed");
  assert.deepEqual(pathA.points, pathB.points);
  assert.equal(pathA.durationMs, pathB.durationMs);

  const typingA = planTyping("hello world", 0.05, createRng("type-seed"));
  const typingB = planTyping("hello world", 0.05, createRng("type-seed"));
  assert.equal(typingA.seed, "type-seed");
  assert.deepEqual(typingA.steps, typingB.steps);
  assert.equal(typingA.typosInjected, typingB.typosInjected);

  const scrollA = planScroll(1800, createRng("scroll-seed"));
  const scrollB = planScroll(1800, createRng("scroll-seed"));
  assert.equal(scrollA.seed, "scroll-seed");
  assert.deepEqual(scrollA.ticks, scrollB.ticks);
});

test("planTyping keeps grapheme clusters and marks them as insert steps", () => {
  const combining = planTyping("e\u0301", 0);
  assert.equal(combining.steps.length, 1);
  assert.equal(combining.steps[0]!.value, "e\u0301");
  assert.equal(combining.steps[0]!.kind, "insert");

  const emoji = planTyping("\u{1F469}\u200D\u{1F4BB}", 1);
  assert.equal(emoji.steps.length, 1);
  assert.equal(emoji.steps[0]!.kind, "insert");
  assert.equal(emoji.typosInjected, 0);
});

/** Two-sample Kolmogorov-Smirnov statistic. */
function ksStatistic(a: number[], b: number[]): number {
  const left = [...a].sort((x, y) => x - y);
  const right = [...b].sort((x, y) => x - y);
  let i = 0;
  let j = 0;
  let statistic = 0;
  while (i < left.length && j < right.length) {
    const value = Math.min(left[i]!, right[j]!);
    while (i < left.length && left[i]! <= value) i += 1;
    while (j < right.length && right[j]! <= value) j += 1;
    statistic = Math.max(statistic, Math.abs(i / left.length - j / right.length));
  }
  return statistic;
}

function ksCritical(n: number, m: number, alpha = 0.05): number {
  // Asymptotic two-sample critical value: c(alpha) * sqrt((n+m)/(n*m)).
  const c = alpha === 0.05 ? 1.36 : 1.63;
  return c * Math.sqrt((n + m) / (n * m));
}

function flightTimes(prefix: string, samples: number): number[] {
  const values: number[] = [];
  for (let index = 0; index < samples; index += 1) {
    const plan = planTyping("x", 0, createRng(`${prefix}-${index}`));
    values.push(plan.steps[0]!.delayMs);
  }
  return values;
}

test("flight times match the same log-normal across seeds (two-sample KS)", () => {
  const first = flightTimes("flight-a", 800);
  const second = flightTimes("flight-b", 800);
  const statistic = ksStatistic(first, second);
  assert.ok(
    statistic < ksCritical(first.length, second.length),
    `KS=${statistic.toFixed(4)} rejected equal distributions`,
  );
});

test("flight times reject a uniform distribution (two-sample KS)", () => {
  const flights = flightTimes("flight-uniform", 800);
  const uniformRng = createRng("uniform-reference");
  const uniform = Array.from({ length: 800 }, () => uniformRng.next() * 300);
  const statistic = ksStatistic(flights, uniform);
  assert.ok(
    statistic > ksCritical(flights.length, uniform.length),
    `KS=${statistic.toFixed(4)} failed to distinguish log-normal from uniform`,
  );
});
