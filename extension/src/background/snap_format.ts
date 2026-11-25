import type { SnapElement } from "../shared/protocol.js";

export const SNAP_HEADER_QUERY_HASH_MAX_CHARS = 80;

/** Truncate overly long query+hash suffixes in SNAP header lines while preserving short URLs. */
export function formatSnapHeaderUrl(rawUrl: string): string {
  if (rawUrl === "") {
    return rawUrl;
  }
  const queryIdx = rawUrl.indexOf("?");
  const hashIdx = rawUrl.indexOf("#");
  let splitIdx = -1;
  if (queryIdx >= 0 && hashIdx >= 0) {
    splitIdx = Math.min(queryIdx, hashIdx);
  } else if (queryIdx >= 0) {
    splitIdx = queryIdx;
  } else if (hashIdx >= 0) {
    splitIdx = hashIdx;
  }
  if (splitIdx < 0) {
    return rawUrl;
  }

  const base = rawUrl.slice(0, splitIdx);
  const suffix = rawUrl.slice(splitIdx);
  if (suffix.length <= SNAP_HEADER_QUERY_HASH_MAX_CHARS) {
    return rawUrl;
  }

  const omitted = suffix.length - SNAP_HEADER_QUERY_HASH_MAX_CHARS;
  return `${base}${suffix.slice(0, SNAP_HEADER_QUERY_HASH_MAX_CHARS)}\u2026 (+${omitted} chars)`;
}

/** Format differential SNAP output, suppressing pure coordinate shifts into a concise moved count. */
export function formatSnapDiff(
  headerLines: string[],
  lastElements: SnapElement[],
  elements: SnapElement[],
  removed: number[],
): string {
  const oldById = new Map<number, SnapElement>();
  for (const el of lastElements) {
    oldById.set(el.id, el);
  }
  const added: string[] = [];
  const changed: string[] = [];
  const gone: string[] = [];
  let movedCount = 0;

  for (const el of elements) {
    const old = oldById.get(el.id);
    if (!old) {
      added.push(`+ [${el.id}] <${el.tag}> "${el.label}" @${el.x},${el.y}`);
    } else if (
      el.change === "updated" ||
      old.tag !== el.tag ||
      old.label !== el.label ||
      old.value !== el.value ||
      old.state !== el.state
    ) {
      changed.push(`~ [${el.id}] <${el.tag}> "${el.label}" @${el.x},${el.y}`);
    } else if (el.change === "moved" || old.x !== el.x || old.y !== el.y) {
      movedCount += 1;
    }
  }
  for (const id of removed) {
    const old = oldById.get(id);
    gone.push(old ? `- [${id}] <${old.tag}> "${old.label}"` : `- [${id}]`);
  }

  const movedSuffix = movedCount > 0 ? `, ${movedCount} moved` : "";
  if (added.length > 0 || changed.length > 0 || gone.length > 0) {
    return [
      ...headerLines,
      `DIFF (+${added.length} added, ~${changed.length} changed, -${gone.length} removed${movedSuffix}):`,
      ...added,
      ...changed,
      ...gone,
    ].join("\n");
  }
  return [
    ...headerLines,
    `DIFF: no interactive elements added, changed, or removed${movedSuffix}.`,
  ].join("\n");
}
