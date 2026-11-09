/**
 * Pure markdown, table, and link extraction for `page.read`.
 *
 * No chrome.* access and no import-time DOM access: this module runs both in the
 * content script bundle and directly under jsdom in tests.
 */

import type { ReadLink, ReadTable } from "../shared/protocol.js";

const SKIP_TAGS = new Set([
  "script",
  "style",
  "noscript",
  "template",
  "svg",
  "canvas",
  "iframe",
]);

const BLOCK_TAGS = new Set([
  "address",
  "article",
  "aside",
  "blockquote",
  "body",
  "dd",
  "details",
  "dialog",
  "div",
  "dl",
  "dt",
  "fieldset",
  "figcaption",
  "figure",
  "footer",
  "form",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "header",
  "hr",
  "li",
  "main",
  "nav",
  "ol",
  "p",
  "pre",
  "section",
  "table",
  "tbody",
  "td",
  "tfoot",
  "th",
  "thead",
  "tr",
  "ul",
]);

const HEADINGS = new Set(["h1", "h2", "h3", "h4", "h5", "h6"]);

const TRUNCATED_MARKER = "\n\n[truncated]";

export const MAX_READ_TABLES = 10;
export const MAX_TABLE_ROWS = 200;
export const MAX_TABLE_CELLS = 50;
export const MAX_READ_LINKS = 200;

function collapseWhitespace(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

function computedStyle(doc: Document, el: Element): CSSStyleDeclaration | null {
  const view = doc.defaultView;
  if (!view) {
    return null;
  }
  try {
    return view.getComputedStyle(el);
  } catch {
    return null;
  }
}

function isHidden(doc: Document, el: Element): boolean {
  const style = computedStyle(doc, el);
  if (!style) {
    return false;
  }
  return (
    style.display === "none" ||
    style.visibility === "hidden" ||
    style.visibility === "collapse"
  );
}

function finishInline(text: string): string {
  return text
    .replace(/[ \t]+/g, " ")
    .replace(/ *\n */g, "\n")
    .replace(/\n{2,}/g, "\n")
    .trim();
}

function inlineChildren(el: Element, doc: Document): string {
  const parts: string[] = [];
  let root: Node = el;
  try {
    if (el.shadowRoot) {
      root = el.shadowRoot;
    }
  } catch {
    root = el;
  }
  for (const node of Array.from(root.childNodes)) {
    parts.push(renderInline(node, doc));
  }
  return finishInline(parts.join(""));
}

function renderLink(el: Element, doc: Document): string {
  const href = (el.getAttribute("href") ?? "").trim();
  const text = inlineChildren(el, doc);
  if (!href) {
    return text;
  }
  return text ? `[${text}](${href})` : href;
}

function renderImage(el: Element): string {
  const src = (el.getAttribute("src") ?? "").trim();
  if (!src) {
    return "";
  }
  const alt = (el.getAttribute("alt") ?? "").replace(/[[\]]/g, "");
  return `![${alt}](${src})`;
}

function renderInline(node: Node, doc: Document): string {
  if (node.nodeType === 3) {
    return (node.nodeValue ?? "").replace(/\s+/g, " ");
  }
  if (node.nodeType !== 1) {
    return "";
  }

  const el = node as Element;
  const tag = el.tagName.toLowerCase();
  if (SKIP_TAGS.has(tag) || isHidden(doc, el)) {
    return "";
  }

  switch (tag) {
    case "br":
      return "\n";
    case "img":
      return renderImage(el);
    case "a":
      return renderLink(el, doc);
    case "strong":
    case "b": {
      const text = inlineChildren(el, doc);
      return text ? `**${text}**` : "";
    }
    case "em":
    case "i": {
      const text = inlineChildren(el, doc);
      return text ? `*${text}*` : "";
    }
    case "code": {
      const text = finishInline(el.textContent ?? "");
      return text ? `\`${text}\`` : "";
    }
    default:
      return inlineChildren(el, doc);
  }
}

function renderList(list: Element, doc: Document, depth: number): string {
  const indent = "  ".repeat(depth);
  const lines: string[] = [];

  for (const child of Array.from(list.children)) {
    if (child.tagName.toLowerCase() !== "li" || isHidden(doc, child)) {
      continue;
    }

    const inlineParts: string[] = [];
    const nested: Element[] = [];
    for (const node of Array.from(child.childNodes)) {
      if (node.nodeType === 1) {
        const tag = (node as Element).tagName.toLowerCase();
        if (tag === "ul" || tag === "ol") {
          nested.push(node as Element);
          continue;
        }
      }
      inlineParts.push(renderInline(node, doc));
    }

    const text = finishInline(inlineParts.join(""));
    lines.push(`${indent}- ${text}`.trimEnd());
    for (const sub of nested) {
      lines.push(renderList(sub, doc, depth + 1));
    }
  }

  return lines.join("\n");
}

function renderQuote(el: Element, doc: Document, depth: number): string {
  const inner = renderBlocks(el, doc, depth).join("\n\n");
  return inner
    .split("\n")
    .map((line) => (line === "" ? ">" : `> ${line}`))
    .join("\n");
}

function renderPre(el: Element): string {
  const code = (el.textContent ?? "").replace(/^\n+/, "").replace(/\n+$/, "");
  if (!code) {
    return "";
  }
  return `\`\`\`\n${code}\n\`\`\``;
}

function renderRow(el: Element, doc: Document): string {
  const cells: string[] = [];
  for (const cell of Array.from(el.children)) {
    const tag = cell.tagName.toLowerCase();
    if (tag === "td" || tag === "th") {
      cells.push(inlineChildren(cell, doc).replace(/\n/g, " "));
    }
  }
  return cells.join(" | ");
}

function renderBlock(el: Element, doc: Document, depth: number): string {
  const tag = el.tagName.toLowerCase();

  if (HEADINGS.has(tag)) {
    const level = Number(tag.slice(1));
    const text = inlineChildren(el, doc);
    return text ? `${"#".repeat(level)} ${text}` : "";
  }
  if (tag === "p") {
    return inlineChildren(el, doc);
  }
  if (tag === "hr") {
    return "---";
  }
  if (tag === "pre") {
    return renderPre(el);
  }
  if (tag === "blockquote") {
    return renderQuote(el, doc, depth);
  }
  if (tag === "ul" || tag === "ol") {
    return renderList(el, doc, depth);
  }
  if (tag === "li") {
    const text = inlineChildren(el, doc);
    return text ? `${"  ".repeat(depth)}- ${text}` : "";
  }
  if (tag === "tr") {
    return renderRow(el, doc);
  }
  return renderBlocks(el, doc, depth).join("\n\n");
}

function collectShadowRoots(
  root: Element | Document | ShadowRoot,
  out: ShadowRoot[] = [],
  depth = 0,
): ShadowRoot[] {
  if (depth > 64) {
    return out;
  }
  for (const el of Array.from(root.querySelectorAll("*"))) {
    let shadow: ShadowRoot | null = null;
    try {
      shadow = el.shadowRoot ?? null;
    } catch {
      shadow = null;
    }
    if (shadow) {
      out.push(shadow);
      collectShadowRoots(shadow, out, depth + 1);
    }
  }
  return out;
}

function collectIframes(
  root: Element | Document | ShadowRoot,
  seenDocs = new Set<Document>(),
  out: HTMLIFrameElement[] = [],
  depth = 0,
): HTMLIFrameElement[] {
  if (depth > 64) {
    return out;
  }
  const elements = Array.from(root.querySelectorAll("*"));
  if ((root as Node).nodeType === 1 && (root as Element).tagName.toLowerCase() === "iframe") {
    elements.unshift(root);
  }
  for (const el of elements) {
    let shadow: ShadowRoot | null = null;
    try {
      shadow = el.shadowRoot ?? null;
    } catch {
      shadow = null;
    }
    if (shadow) {
      collectIframes(shadow, seenDocs, out, depth + 1);
    }
    if (el.tagName.toLowerCase() === "iframe") {
      out.push(el as HTMLIFrameElement);
    }
  }
  return out;
}

function renderBlocks(root: Element | ShadowRoot, doc: Document, depth: number): string[] {
  const blocks: string[] = [];
  let inlineBuffer: string[] = [];

  const flush = (): void => {
    if (inlineBuffer.length === 0) {
      return;
    }
    const text = finishInline(inlineBuffer.join(""));
    if (text) {
      blocks.push(text);
    }
    inlineBuffer = [];
  };

  for (const node of Array.from(root.childNodes)) {
    if (node.nodeType === 3) {
      inlineBuffer.push(renderInline(node, doc));
      continue;
    }
    if (node.nodeType !== 1) {
      continue;
    }

    const el = node as Element;
    const tag = el.tagName.toLowerCase();
    if (SKIP_TAGS.has(tag) || isHidden(doc, el)) {
      continue;
    }

    let shadow: ShadowRoot | null = null;
    try {
      shadow = el.shadowRoot ?? null;
    } catch {
      shadow = null;
    }

    if (shadow) {
      flush();
      const shadowBlocks = renderBlocks(shadow, doc, depth);
      if (shadowBlocks.length > 0) {
        blocks.push(...shadowBlocks);
      }
      continue;
    }

    if (BLOCK_TAGS.has(tag)) {
      flush();
      const block = renderBlock(el, doc, depth);
      if (block) {
        blocks.push(block);
      }
      continue;
    }

    inlineBuffer.push(renderInline(node, doc));
  }

  flush();

  return blocks;
}

function cleanBlock(block: string): string {
  return block
    .split("\n")
    .map((line) => line.replace(/[ \t]+$/, ""))
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

/** Pick the densest main-content container, falling back to <body>. */
export function pickReadRoot(doc: Document): Element {
  const roots: (Document | ShadowRoot)[] = [doc, ...collectShadowRoots(doc)];
  const candidates: Element[] = [];
  for (const r of roots) {
    try {
      candidates.push(...Array.from(r.querySelectorAll('main, article, [role="main"]')));
    } catch {
      // ignore
    }
  }
  let best: Element | null = null;
  let bestLength = 0;

  for (const candidate of candidates) {
    const length = collapseWhitespace(candidate.textContent).length;
    if (length > bestLength) {
      best = candidate;
      bestLength = length;
    }
  }

  if (best) {
    return best;
  }
  return doc.body ?? doc.documentElement;
}

/** Convert a document (or a subtree) to plain-text markdown. Never emits HTML. */
export function extractMarkdown(doc: Document, root?: Element): string {
  const target = root ?? pickReadRoot(doc);
  const blocks = renderBlocks(target, doc, 0)
    .map(cleanBlock)
    .filter((block) => block !== "");

  let targetShadow: ShadowRoot | null = null;
  try {
    targetShadow = target.shadowRoot ?? null;
  } catch {
    targetShadow = null;
  }
  if (targetShadow) {
    const shadowBlocks = renderBlocks(targetShadow, doc, 0)
      .map(cleanBlock)
      .filter((block) => block !== "");
    for (const sb of shadowBlocks) {
      if (!blocks.includes(sb)) {
        blocks.push(sb);
      }
    }
  }

  // Pierce same-origin iframes in extractMarkdown
  const seenDocs = new Set<Document>([doc]);
  const iframes = collectIframes(target, seenDocs);
  for (const iframe of iframes) {
    if (isHidden(doc, iframe)) {
      continue;
    }
    let iframeDoc: Document | null = null;
    try {
      iframeDoc = iframe.contentDocument ?? null;
    } catch {
      iframeDoc = null;
    }
    if (iframeDoc && !seenDocs.has(iframeDoc)) {
      seenDocs.add(iframeDoc);
      const subMarkdown = extractMarkdown(iframeDoc);
      if (subMarkdown) {
        blocks.push(subMarkdown);
      }
    }
  }

  return blocks.join("\n\n").replace(/\n{3,}/g, "\n\n").trim();
}

/** Cap markdown length and flag the cut. */
export function truncateMarkdown(markdown: string, maxChars: number): string {
  if (markdown.length <= maxChars) {
    return markdown;
  }
  return `${markdown.slice(0, maxChars)}${TRUNCATED_MARKER}`;
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Resolve a read target: numeric snapshot id, CSS selector, or the page's
 * main-content container.
 */
export function resolveReadElement(
  doc: Document,
  index: Map<number, Element> | undefined,
  id?: number,
  selector?: string,
): Element {
  if (typeof id === "number") {
    const element = index?.get(id);
    if (!element || !element.isConnected) {
      throw new Error(`element id ${id} not found; run snap first`);
    }
    return element;
  }

  if (typeof selector === "string" && selector.trim() !== "") {
    let element: Element | null;
    try {
      element = doc.querySelector(selector);
    } catch (error) {
      throw new Error(`invalid selector "${selector}": ${errorText(error)}`);
    }
    if (!element) {
      throw new Error(`selector not found: ${selector}`);
    }
    return element;
  }

  return pickReadRoot(doc);
}

export interface ExtractedTables {
  tables: ReadTable[];
  truncated: boolean;
}

function rowCells(row: Element): Element[] {
  return Array.from(row.children).filter((cell) => {
    const tag = cell.tagName.toLowerCase();
    return tag === "td" || tag === "th";
  });
}

function headerCells(row: Element): string[] {
  const headers = rowCells(row)
    .filter((cell) => cell.tagName.toLowerCase() === "th")
    .map((cell) => collapseWhitespace(cell.textContent));
  if (headers.length > 0) {
    return headers.slice(0, MAX_TABLE_CELLS);
  }
  return rowCells(row)
    .slice(0, MAX_TABLE_CELLS)
    .map((cell) => collapseWhitespace(cell.textContent));
}

function tableRows(table: Element): Element[] {
  // Nested tables have their own rows; keep each table's extraction isolated.
  return Array.from(table.querySelectorAll("tr")).filter(
    (row) => row.closest("table") === table,
  );
}

function extractTable(table: Element, index: number): ReadTable | null {
  const rows = tableRows(table);
  if (rows.length === 0) {
    return null;
  }

  let headers: string[] = [];
  let start = 0;

  const theadRow = rows.find((row) => row.closest("thead")?.closest("table") === table);
  if (theadRow) {
    headers = headerCells(theadRow);
    start = rows.indexOf(theadRow) + 1;
  } else {
    const thRowIndex = rows.findIndex((row) => row.querySelector("th") !== null);
    if (thRowIndex >= 0) {
      headers = headerCells(rows[thRowIndex]);
      start = thRowIndex + 1;
    } else {
      headers = headerCells(rows[0]);
      start = 1;
    }
  }

  const body: string[][] = [];
  for (let cursor = start; cursor < rows.length && body.length < MAX_TABLE_ROWS; cursor += 1) {
    const cells = rowCells(rows[cursor])
      .slice(0, MAX_TABLE_CELLS)
      .map((cell) => collapseWhitespace(cell.textContent));
    if (cells.length === 0) {
      continue;
    }
    body.push(cells);
  }

  if (headers.length === 0 && body.length === 0) {
    return null;
  }

  const caption = collapseWhitespace(table.querySelector("caption")?.textContent);
  const result: ReadTable = {
    index,
    headers,
    rows: body,
  };
  if (caption !== "") {
    result.caption = caption;
  }
  return result;
}

/** Extract tables from a document or subtree as header + row arrays. */
export function extractTables(
  doc: Document,
  root?: Element,
  maxTables = MAX_READ_TABLES,
): ExtractedTables {
  const target = root ?? doc.body ?? doc.documentElement;
  const limit = Math.max(1, Math.min(MAX_READ_TABLES, maxTables));
  const tables: ReadTable[] = [];
  let truncated = false;

  const found: Element[] = Array.from(target.querySelectorAll("table"));
  if (target.tagName.toLowerCase() === "table") {
    found.unshift(target);
  }
  const allTables = found.filter((table) => !isHidden(doc, table));
  for (const table of allTables) {
    if (tables.length >= limit) {
      truncated = true;
      break;
    }
    const extracted = extractTable(table, tables.length + 1);
    if (extracted) {
      tables.push(extracted);
      if (extracted.rows.length >= MAX_TABLE_ROWS) {
        truncated = true;
      }
    }
  }

  if (allTables.length > limit) {
    truncated = true;
  }

  return { tables, truncated };
}

export interface ExtractedLinks {
  links: ReadLink[];
  truncated: boolean;
}

function absoluteHref(doc: Document, raw: string): string {
  try {
    return new URL(raw, doc.baseURI).href;
  } catch {
    return raw;
  }
}

/** Extract visible links as {text, absoluteHref}, deduped by href. */
export function extractLinks(
  doc: Document,
  root?: Element,
  maxLinks = MAX_READ_LINKS,
): ExtractedLinks {
  const target = root ?? doc.body ?? doc.documentElement;
  const limit = Math.max(1, Math.min(MAX_READ_LINKS, maxLinks));
  const links: ReadLink[] = [];
  const seen = new Set<string>();
  let truncated = false;

  const anchors: Element[] = Array.from(target.querySelectorAll("a[href]"));
  if (target.tagName.toLowerCase() === "a" && target.hasAttribute("href")) {
    anchors.unshift(target);
  }

  for (const anchor of anchors) {
    if (isHidden(doc, anchor)) {
      continue;
    }
    const raw = (anchor.getAttribute("href") ?? "").trim();
    const hasOnClick = anchor.hasAttribute("onclick");
    const isJs = /^javascript:/i.test(raw);
    if (raw === "" || raw.startsWith("#") || (isJs && !hasOnClick)) {
      continue;
    }
    const href = absoluteHref(doc, raw);
    if (seen.has(href)) {
      continue;
    }
    seen.add(href);

    if (links.length >= limit) {
      truncated = true;
      break;
    }

    const text =
      collapseWhitespace(anchor.textContent) ||
      collapseWhitespace(anchor.getAttribute("aria-label")) ||
      collapseWhitespace(anchor.getAttribute("title")) ||
      collapseWhitespace(anchor.querySelector("img[alt]")?.getAttribute("alt")) ||
      href;
    links.push({ text, href });
  }

  return { links, truncated };
}
