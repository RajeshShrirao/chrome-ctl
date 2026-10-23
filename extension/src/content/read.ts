/**
 * Pure markdown extraction for `page.read`.
 *
 * No chrome.* access and no import-time DOM access: this module runs both in the
 * content script bundle and directly under jsdom in tests.
 */

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
  for (const node of Array.from(el.childNodes)) {
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

function renderBlocks(root: Element, doc: Document, depth: number): string[] {
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
  const candidates = Array.from(doc.querySelectorAll('main, article, [role="main"]'));
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
