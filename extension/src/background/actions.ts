/**
 * Phase 3 action executors. Each executor composes content-script state
 * probing with trusted CDP input dispatch.
 *
 * The content sender is injected by the service worker so this module has no
 * dependency on its routing internals.
 */

import { CONTENT_CHANNEL } from "../shared/content.js";
import type {
  ContentMetricsPayload,
  ContentRequest,
  ContentResponse,
  ContentStateEntry,
  ContentTargetPayload,
} from "../shared/content.js";
import { ErrorCodes } from "../shared/protocol.js";
import type {
  ActionClickResult,
  ActionPressResult,
  ActionScrollResult,
  ActionTypeResult,
  ActionUploadResult,
} from "../shared/protocol.js";
import * as debuggerController from "./debugger.js";
import { RpcFault, errorText } from "./faults.js";
import {
  clickLanding,
  keyDefinition,
  modifiersMask,
  planMousePath,
  planScroll,
  planTyping,
  randomInt,
} from "./humanizer.js";
import type { ClickRect, KeyDefinition, Point2D } from "./humanizer.js";

export { RpcFault } from "./faults.js";

export type ContentSender = (
  tabId: number,
  request: ContentRequest,
) => Promise<ContentResponse>;

export interface ClickOptions {
  doubleClick?: boolean;
  sendContent: ContentSender;
}

export interface TypeOptions {
  enter?: boolean;
  typoRate?: number;
  sendContent: ContentSender;
}

export interface ScrollOptions {
  direction: "down" | "up";
  amount: number;
  sendContent: ContentSender;
}

export interface UploadOptions {
  filePath: string;
  sendContent: ContentSender;
}

const MAX_TEXT_CHARS = 5000;
const DEFAULT_TYPO_RATE = 0.02;

interface Pointer {
  x: number;
  y: number;
}

/** Last pointer position per tab, shared by click and scroll. */
const lastPointer = new Map<number, Pointer>();

function sleep(ms: number): Promise<void> {
  const delay = Math.max(0, Math.round(ms));
  if (delay === 0) {
    return Promise.resolve();
  }
  return new Promise((resolve) => setTimeout(resolve, delay));
}

async function run<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (error) {
    if (error instanceof RpcFault) {
      throw error;
    }
    throw new RpcFault(ErrorCodes.internal, errorText(error));
  }
}

function unexpectedResponse(): RpcFault {
  return new RpcFault(ErrorCodes.internal, "unexpected content script response");
}

async function requestTarget(
  sendContent: ContentSender,
  tabId: number,
  id: number,
): Promise<ContentTargetPayload> {
  const response = await sendContent(tabId, {
    channel: CONTENT_CHANNEL,
    type: "target",
    id,
  });
  if (!response.ok) {
    throw new RpcFault(ErrorCodes.elementNotFound, response.error);
  }
  if (response.type !== "target") {
    throw unexpectedResponse();
  }
  return response.target;
}

async function requestMetrics(
  sendContent: ContentSender,
  tabId: number,
): Promise<ContentMetricsPayload> {
  const response = await sendContent(tabId, {
    channel: CONTENT_CHANNEL,
    type: "metrics",
  });
  if (!response.ok) {
    throw new RpcFault(ErrorCodes.internal, response.error);
  }
  if (response.type !== "metrics") {
    throw unexpectedResponse();
  }
  return response.metrics;
}

async function requestState(
  sendContent: ContentSender,
  tabId: number,
): Promise<ContentStateEntry[]> {
  const response = await sendContent(tabId, {
    channel: CONTENT_CHANNEL,
    type: "state",
  });
  if (!response.ok) {
    throw new RpcFault(ErrorCodes.internal, response.error);
  }
  if (response.type !== "state") {
    throw unexpectedResponse();
  }
  return response.entries;
}

function requireInteractable(target: ContentTargetPayload, id: number): void {
  if (!target.interactable) {
    throw new RpcFault(
      ErrorCodes.elementNotInteractable,
      `element [${id}] is not interactable (${target.reason})`,
    );
  }
}

function mouseEventParams(
  type: string,
  x: number,
  y: number,
  extra: Record<string, unknown> = {},
): Record<string, unknown> {
  return {
    type,
    x: Math.round(x),
    y: Math.round(y),
    pointerType: "mouse",
    ...extra,
  };
}

function keyEventParams(
  definition: KeyDefinition,
  type: string,
  modifiers: number,
): Record<string, unknown> {
  const params: Record<string, unknown> = {
    type,
    key: definition.key,
    code: definition.code,
    windowsVirtualKeyCode: definition.windowsVirtualKeyCode,
    nativeVirtualKeyCode: definition.windowsVirtualKeyCode,
  };
  if (modifiers !== 0) {
    params.modifiers = modifiers;
  }
  if (type !== "keyUp" && definition.text !== undefined) {
    params.text = definition.text;
    params.unmodifiedText = definition.text;
  }
  return params;
}

async function pressKey(tabId: number, definition: KeyDefinition, modifiers: number): Promise<void> {
  const type = definition.text !== undefined ? "keyDown" : "rawKeyDown";
  await debuggerController.send(tabId, "Input.dispatchKeyEvent", keyEventParams(definition, type, modifiers));
  await sleep(randomInt(45, 85));
  await debuggerController.send(tabId, "Input.dispatchKeyEvent", keyEventParams(definition, "keyUp", modifiers));
}

function stateDelta(before: ContentStateEntry[], after: ContentStateEntry[]): string {
  const previous = new Map(before.map((entry) => [entry.id, entry]));
  const changes: string[] = [];

  for (const entry of after) {
    const old = previous.get(entry.id);
    if (!old || old.disabled === entry.disabled) {
      continue;
    }
    const status = entry.disabled ? "disabled" : "enabled";
    changes.push(`[${entry.id}] <${entry.tag}:${entry.type}> "${entry.label}" is now (${status})`);
  }

  return changes.join("; ");
}

export async function click(
  tabId: number,
  id: number,
  options: ClickOptions,
): Promise<ActionClickResult> {
  return run(async () => {
    const startedAt = Date.now();
    const target = await requestTarget(options.sendContent, tabId, id);
    requireInteractable(target, id);

    const rect: ClickRect = {
      left: target.x,
      top: target.y,
      width: target.w,
      height: target.h,
    };
    const landing = clickLanding(rect);

    let start: Point2D | undefined = lastPointer.get(tabId);
    if (!start) {
      const metrics = await requestMetrics(options.sendContent, tabId);
      start = { x: metrics.viewportWidth / 2, y: metrics.viewportHeight / 2 };
    }

    const path = planMousePath(start, landing, target.w);
    for (const point of path.points) {
      await debuggerController.send(
        tabId,
        "Input.dispatchMouseEvent",
        mouseEventParams("mouseMoved", point.x, point.y),
      );
      await sleep(point.delayMs);
    }

    await sleep(randomInt(30, 80));

    const x = Math.round(landing.x);
    const y = Math.round(landing.y);

    await debuggerController.send(
      tabId,
      "Input.dispatchMouseEvent",
      mouseEventParams("mousePressed", x, y, { button: "left", clickCount: 1 }),
    );
    await sleep(randomInt(45, 95));
    await debuggerController.send(
      tabId,
      "Input.dispatchMouseEvent",
      mouseEventParams("mouseReleased", x, y, { button: "left", clickCount: 1 }),
    );

    if (options.doubleClick === true) {
      await sleep(randomInt(70, 140));
      await debuggerController.send(
        tabId,
        "Input.dispatchMouseEvent",
        mouseEventParams("mousePressed", x, y, { button: "left", clickCount: 2 }),
      );
      await sleep(randomInt(45, 95));
      await debuggerController.send(
        tabId,
        "Input.dispatchMouseEvent",
        mouseEventParams("mouseReleased", x, y, { button: "left", clickCount: 2 }),
      );
    }

    lastPointer.set(tabId, { x, y });

    return {
      success: true,
      action: "click",
      target: { id, tag: target.tag, label: target.label },
      coordinates: { x, y },
      durationMs: Date.now() - startedAt,
    };
  });
}

export async function typeText(
  tabId: number,
  id: number,
  text: string,
  options: TypeOptions,
): Promise<ActionTypeResult> {
  return run(async () => {
    const startedAt = Date.now();

    if (typeof text !== "string") {
      throw new RpcFault(ErrorCodes.internal, "text is required");
    }
    if (text.length > MAX_TEXT_CHARS) {
      throw new RpcFault(
        ErrorCodes.internal,
        `text is too long (max ${MAX_TEXT_CHARS} chars)`,
      );
    }

    const typoRate = options.typoRate === undefined ? DEFAULT_TYPO_RATE : options.typoRate;
    if (typeof typoRate !== "number" || !Number.isFinite(typoRate) || typoRate < 0 || typoRate > 1) {
      throw new RpcFault(ErrorCodes.internal, "typoRate must be between 0 and 1");
    }

    const target = await requestTarget(options.sendContent, tabId, id);
    if (!target.editable) {
      throw new RpcFault(
        ErrorCodes.elementNotInteractable,
        `element [${id}] is not a text field`,
      );
    }

    const before = await requestState(options.sendContent, tabId);

    const focusResponse = await options.sendContent(tabId, {
      channel: CONTENT_CHANNEL,
      type: "focus",
      id,
    });
    if (!focusResponse.ok) {
      throw new RpcFault(ErrorCodes.elementNotFound, focusResponse.error);
    }

    const plan = planTyping(text, typoRate);

    for (const step of plan.steps) {
      if (step.kind === "char") {
        let definition: KeyDefinition;
        try {
          definition = keyDefinition(step.value);
        } catch {
          // Non-QWERTY characters (accents, CJK, emoji) arrive as trusted IME text.
          await debuggerController.send(tabId, "Input.insertText", {
            text: step.value,
          });
          await sleep(step.delayMs);
          continue;
        }
        const modifiers = definition.shift === true ? 8 : 0;
        await debuggerController.send(
          tabId,
          "Input.dispatchKeyEvent",
          keyEventParams(definition, "keyDown", modifiers),
        );
        await sleep(step.holdMs);
        await debuggerController.send(
          tabId,
          "Input.dispatchKeyEvent",
          keyEventParams(definition, "keyUp", modifiers),
        );
      } else {
        const definition = keyDefinition(step.value);
        await debuggerController.send(
          tabId,
          "Input.dispatchKeyEvent",
          keyEventParams(definition, "rawKeyDown", 0),
        );
        await sleep(step.holdMs);
        await debuggerController.send(
          tabId,
          "Input.dispatchKeyEvent",
          keyEventParams(definition, "keyUp", 0),
        );
      }
      await sleep(step.delayMs);
    }

    if (options.enter === true) {
      await pressKey(tabId, keyDefinition("Enter"), 0);
    }

    const after = await requestState(options.sendContent, tabId);

    return {
      success: true,
      charsTyped: text.length,
      durationMs: Date.now() - startedAt,
      typosInjected: plan.typosInjected,
      stateDelta: stateDelta(before, after),
    };
  });
}

export async function press(
  tabId: number,
  key: string,
  modifiers: string[] = [],
): Promise<ActionPressResult> {
  return run(async () => {
    const definition = keyDefinition(key);
    const modifierBits = modifiersMask(modifiers) | (definition.shift === true ? 8 : 0);
    await pressKey(tabId, definition, modifierBits);
    return { success: true, key };
  });
}

export async function scroll(
  tabId: number,
  options: ScrollOptions,
): Promise<ActionScrollResult> {
  return run(async () => {
    if (options.direction !== "down" && options.direction !== "up") {
      throw new RpcFault(ErrorCodes.internal, 'direction must be "down" or "up"');
    }
    if (
      typeof options.amount !== "number" ||
      !Number.isFinite(options.amount) ||
      options.amount <= 0
    ) {
      throw new RpcFault(ErrorCodes.internal, "amount must be greater than 0");
    }

    const before = await requestMetrics(options.sendContent, tabId);

    let pointer = lastPointer.get(tabId);
    const pointerOutsideViewport =
      pointer !== undefined &&
      (pointer.x < 0 ||
        pointer.y < 0 ||
        pointer.x > before.viewportWidth ||
        pointer.y > before.viewportHeight);

    if (pointer === undefined || pointerOutsideViewport) {
      pointer = {
        x: Math.round(before.viewportWidth / 2),
        y: Math.round(before.viewportHeight / 2),
      };
      await debuggerController.send(
        tabId,
        "Input.dispatchMouseEvent",
        mouseEventParams("mouseMoved", pointer.x, pointer.y),
      );
      lastPointer.set(tabId, pointer);
    }

    const distance =
      Math.round(options.amount * before.viewportHeight) *
      (options.direction === "up" ? -1 : 1);
    const plan = planScroll(distance);

    for (const tick of plan.ticks) {
      await debuggerController.send(
        tabId,
        "Input.dispatchMouseEvent",
        mouseEventParams("mouseWheel", pointer.x, pointer.y, {
          deltaX: 0,
          deltaY: tick.delta,
        }),
      );
      await sleep(tick.delayMs);
      if (tick.pauseMs > 0) {
        await sleep(tick.pauseMs);
      }
    }

    await sleep(250);

    const after = await requestMetrics(options.sendContent, tabId);

    return {
      scrolledPixels: Math.round(Math.abs(after.scrollY - before.scrollY)),
      newScrollY: Math.round(after.scrollY),
      viewportPercent: after.percent,
    };
  });
}

function baseName(filePath: string): string {
  const slash = Math.max(filePath.lastIndexOf("/"), filePath.lastIndexOf("\\"));
  return slash >= 0 ? filePath.slice(slash + 1) : filePath;
}

interface RemoteObject {
  type?: string;
  subtype?: string;
  objectId?: string;
}

interface EvaluateResult {
  result?: RemoteObject;
  exceptionDetails?: unknown;
}

export async function upload(
  tabId: number,
  id: number,
  options: UploadOptions,
): Promise<ActionUploadResult> {
  return run(async () => {
    const target = await requestTarget(options.sendContent, tabId, id);
    if (target.tag !== "input" || target.type !== "file") {
      throw new RpcFault(
        ErrorCodes.elementNotInteractable,
        `element [${id}] is not a file input`,
      );
    }

    const expression = `document.querySelector(${JSON.stringify(target.cssPath)})`;
    const evaluated = await debuggerController.send<EvaluateResult>(
      tabId,
      "Runtime.evaluate",
      { expression, returnByValue: false },
    );

    const remote = evaluated?.result;
    if (!remote || !remote.objectId) {
      throw new RpcFault(
        ErrorCodes.elementNotFound,
        `element [${id}] not found; run snap first`,
      );
    }

    await debuggerController.send(tabId, "DOM.setFileInputFiles", {
      files: [options.filePath],
      objectId: remote.objectId,
    });

    return {
      success: true,
      fileName: baseName(options.filePath),
      fileSize: 0,
    };
  });
}
