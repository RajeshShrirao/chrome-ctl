/**
 * Stable element identity across snapshots.
 *
 * A snapshot is re-rendered on every poll, so minting fresh ids each time forces
 * callers to rebuild their map. This module assigns ids deterministically:
 *
 *   1. Same DOM node seen before -> reuse its id (strongest signal).
 *   2. Otherwise bucket previous elements by fingerprint; a unique match reuses
 *      the id, and collisions are resolved by viewport distance.
 *   3. Otherwise keep the freshly minted id.
 *
 * Each emitted element also reports how it changed since the previous snapshot:
 * `added`, `same`, `updated`, `moved`, or (via the payload's `removed` list)
 * gone from the document.
 *
 * Pure: no chrome.* access and no import-time DOM reads.
 */

import type { SnapElement } from "../shared/protocol.js";
import { roleFor, testIdFor } from "./describe.ts";

export type SnapChange = "added" | "same" | "updated" | "moved";

export interface IdentityRecord {
  id: number;
  el: Element;
  /** Last emitted view (already carrying the stable id). */
  element: SnapElement;
  fingerprint: string;
  /** Snapshot id in which this element was last emitted. */
  lastSnapshotId: number;
}

export interface IdentityState {
  records: Map<number, IdentityRecord>;
  lastSnapshotId: number;
}

export interface IdentityResult {
  elements: SnapElement[];
  byId: Map<number, Element>;
  /** Ids that disappeared from the document since the previous snapshot. */
  removed: number[];
  state: IdentityState;
}

export function emptyIdentity(): IdentityState {
  return { records: new Map(), lastSnapshotId: 0 };
}

const FNV_OFFSET = 0xcbf29ce484222325n;
const FNV_PRIME = 0x100000001b3n;
const MASK64 = 0xffffffffffffffffn;

/** FNV-1a 64-bit as 16 hex chars; deterministic and dependency-free. */
export function fingerprintHash(input: string): string {
  let hash = FNV_OFFSET;
  for (let index = 0; index < input.length; index += 1) {
    hash ^= BigInt(input.charCodeAt(index));
    hash = (hash * FNV_PRIME) & MASK64;
  }
  return hash.toString(16).padStart(16, "0");
}

function composedParent(el: Element): Element | null {
  if (el.parentElement !== null) {
    return el.parentElement;
  }
  const root = el.getRootNode();
  if (root.nodeType === 11) {
    return (root as ShadowRoot).host ?? null;
  }
  return null;
}

function nthOfType(el: Element): number {
  let index = 1;
  let sibling = el.previousElementSibling;
  while (sibling !== null) {
    if (sibling.tagName === el.tagName) {
      index += 1;
    }
    sibling = sibling.previousElementSibling;
  }
  return index;
}

/** Tags plus nth-of-type for at most `depth` composed ancestors. */
function ancestrySignature(el: Element, depth = 8): string {
  const parts: string[] = [];
  let current: Element | null = el;
  for (let level = 0; current !== null && level < depth; level += 1) {
    parts.push(`${current.tagName.toLowerCase()}:${nthOfType(current)}`);
    current = composedParent(current);
  }
  return parts.reverse().join("/");
}

/** Frame path by frame-element tags; separates identical controls per frame. */
function frameSignature(el: Element): string {
  const parts: string[] = [];
  let doc = el.ownerDocument;
  for (let depth = 0; depth < 16; depth += 1) {
    let frame: Element | null = null;
    try {
      frame = doc.defaultView?.frameElement ?? null;
    } catch {
      frame = null;
    }
    if (frame === null) {
      break;
    }
    parts.push(`${frame.tagName.toLowerCase()}:${nthOfType(frame)}`);
    doc = frame.ownerDocument;
  }
  return parts.reverse().join("/");
}

function pathOf(href: string): string {
  if (href === "") {
    return "";
  }
  try {
    const parsed = new URL(href, "https://chrome-ctl.invalid/");
    return parsed.pathname;
  } catch {
    return href;
  }
}

/**
 * Deterministic fingerprint from stable semantics, never geometry or values
 * that change while the user types. Used only to match across node replacement.
 */
export function elementFingerprint(el: Element, element: SnapElement): string {
  const tag = el.tagName.toLowerCase();
  const type = tag === "input" ? (el.getAttribute("type") ?? "").trim().toLowerCase() : "";
  const parts = [
    roleFor(el),
    element.label,
    testIdFor(el),
    tag,
    type,
    (el.getAttribute("name") ?? "").trim(),
    pathOf(el.getAttribute("href") ?? ""),
    (el.getAttribute("aria-controls") ?? "").trim(),
    frameSignature(el),
    ancestrySignature(el),
  ];
  return fingerprintHash(parts.join("\u0001"));
}

function center(element: SnapElement): { x: number; y: number } {
  return { x: element.x + element.w / 2, y: element.y + element.h / 2 };
}

function distanceSquared(a: SnapElement, b: SnapElement): number {
  const left = center(a);
  const right = center(b);
  const dx = left.x - right.x;
  const dy = left.y - right.y;
  return dx * dx + dy * dy;
}

function classify(previous: SnapElement | undefined, next: SnapElement): SnapChange {
  if (previous === undefined) {
    return "added";
  }
  if (
    previous.tag !== next.tag ||
    previous.type !== next.type ||
    previous.label !== next.label ||
    previous.value !== next.value ||
    previous.state !== next.state
  ) {
    return "updated";
  }
  if (
    previous.x !== next.x ||
    previous.y !== next.y ||
    previous.w !== next.w ||
    previous.h !== next.h
  ) {
    return "moved";
  }
  return "same";
}

/**
 * Re-key a fresh snapshot onto stable ids and compute per-element changes.
 * `byId` must be the snapshot's minted id -> element map.
 */
export function applyIdentity(
  elements: SnapElement[],
  byId: Map<number, Element>,
  state: IdentityState,
  snapshotId: number,
): IdentityResult {
  const nodeToRecord = new Map<Element, IdentityRecord>();
  const buckets = new Map<string, IdentityRecord[]>();
  for (const record of state.records.values()) {
    nodeToRecord.set(record.el, record);
    const bucket = buckets.get(record.fingerprint);
    if (bucket === undefined) {
      buckets.set(record.fingerprint, [record]);
    } else {
      bucket.push(record);
    }
  }

  const claimed = new Set<number>();
  const nextElements: SnapElement[] = [];
  const nextById = new Map<number, Element>();
  const nextRecords = new Map<number, IdentityRecord>();

  for (const element of elements) {
    const el = byId.get(element.id);
    if (el === undefined) {
      continue;
    }
    const fingerprint = elementFingerprint(el, element);

    let match = nodeToRecord.get(el);
    if (match !== undefined && claimed.has(match.id)) {
      match = undefined;
    }
    if (match === undefined) {
      const candidates = (buckets.get(fingerprint) ?? []).filter(
        (record) => !claimed.has(record.id),
      );
      if (candidates.length === 1) {
        match = candidates[0];
      } else if (candidates.length > 1) {
        match = candidates.reduce((best, candidate) =>
          distanceSquared(candidate.element, element) < distanceSquared(best.element, element)
            ? candidate
            : best,
        );
      }
    }

    const stableId = match?.id ?? element.id;
    if (match !== undefined) {
      claimed.add(match.id);
    }

    const stabilised: SnapElement = {
      ...element,
      id: stableId,
      change: classify(match?.element, element),
    };
    nextElements.push(stabilised);
    nextById.set(stableId, el);
    nextRecords.set(stableId, {
      id: stableId,
      el,
      element: stabilised,
      fingerprint,
      lastSnapshotId: snapshotId,
    });
  }

  const removed: number[] = [];
  for (const record of state.records.values()) {
    if (nextRecords.has(record.id)) {
      continue;
    }
    if (record.el.isConnected) {
      // Culled from this snapshot (for example offscreen without --all) but
      // still in the document: keep its identity for the next snapshot.
      nextRecords.set(record.id, record);
    } else {
      removed.push(record.id);
    }
  }
  removed.sort((left, right) => left - right);

  return {
    elements: nextElements,
    byId: nextById,
    removed,
    state: { records: nextRecords, lastSnapshotId: snapshotId },
  };
}
