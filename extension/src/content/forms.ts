/** Form-control helpers shared by the snapshot indexer (Phase 2 `--forms`). */

import type { FormControlState } from "../shared/protocol.js";

export function effectiveInputType(el: Element): string {
  const raw = (el.getAttribute("type") ?? "").trim().toLowerCase();
  return raw === "" ? "text" : raw;
}

export function isSubmitLike(el: Element): boolean {
  const tag = el.tagName.toLowerCase();
  if (tag === "input") {
    const type = effectiveInputType(el);
    return type === "submit" || type === "image";
  }
  if (tag === "button") {
    const type = el.getAttribute("type");
    if (type === null) {
      return el.closest("form") !== null;
    }
    return type.trim().toLowerCase() === "submit";
  }
  return false;
}

/** Inputs, selects, textareas, contenteditable regions, and submit-like buttons. */
export function isFormField(el: Element): boolean {
  const tag = el.tagName.toLowerCase();
  if (tag === "input") {
    return effectiveInputType(el) !== "hidden";
  }
  if (tag === "textarea" || tag === "select") {
    return true;
  }
  if (el.getAttribute("contenteditable") === "true") {
    return true;
  }
  return isSubmitLike(el);
}

function getLabel(el: Element, doc: Document): string {
  const ariaLabel = (el.getAttribute("aria-label") ?? "").trim();
  if (ariaLabel) return ariaLabel;

  const labelledBy = (el.getAttribute("aria-labelledby") ?? "").trim();
  if (labelledBy) {
    const parts = labelledBy
      .split(/\s+/)
      .map((id) => doc.getElementById(id)?.textContent?.trim())
      .filter(Boolean);
    if (parts.length > 0) return parts.join(" ");
  }

  const id = el.getAttribute("id");
  if (id) {
    try {
      const label = doc.querySelector(`label[for="${id}"]`);
      if (label && label.textContent?.trim()) {
        return label.textContent.trim().replace(/\s+/g, " ");
      }
    } catch {
      // ignore
    }
  }

  const wrappingLabel = el.closest("label");
  if (wrappingLabel && wrappingLabel.textContent?.trim()) {
    return wrappingLabel.textContent.trim().replace(/\s+/g, " ");
  }

  const placeholder = (el.getAttribute("placeholder") ?? "").trim();
  if (placeholder) return placeholder;

  const title = (el.getAttribute("title") ?? "").trim();
  if (title) return title;

  return (el.textContent ?? "").trim().replace(/\s+/g, " ");
}

/** Extract all form controls across top document, shadow roots, and same-origin iframes. */
export function extractFormState(doc: Document): FormControlState[] {
  const controls: FormControlState[] = [];
  const seen = new Set<Element>();
  let frameCounter = 0;

  function traverse(root: Document | ShadowRoot, framePath: string, depth = 0) {
    if (depth > 64) return;
    const elements = Array.from(root.querySelectorAll("*"));
    for (const el of elements) {
      if (seen.has(el)) continue;

      const tag = el.tagName.toLowerCase();
      const role = (el.getAttribute("role") ?? "").toLowerCase();

      const isInput = tag === "input";
      const isSelect = tag === "select";
      const isTextarea = tag === "textarea";
      const isButton = tag === "button" || isSubmitLike(el);
      const isAriaControl =
        role === "radio" || role === "checkbox" || role === "combobox" || role === "textbox";

      if (isInput || isSelect || isTextarea || isButton || isAriaControl) {
        seen.add(el);
        const type = isInput
          ? effectiveInputType(el)
          : isButton
            ? "button"
            : role || tag;

        let checked: boolean | undefined = undefined;
        if (isInput && (type === "radio" || type === "checkbox")) {
          checked = (el as HTMLInputElement).checked === true;
        } else if (role === "radio" || role === "checkbox") {
          checked = el.getAttribute("aria-checked") === "true";
        }

        let value = "";
        if (isInput) {
          value = (el as HTMLInputElement).value ?? "";
        } else if (isSelect) {
          const sel = el as HTMLSelectElement;
          const opt = sel.options?.[sel.selectedIndex];
          value = opt ? opt.text || opt.value : sel.value ?? "";
        } else if (isTextarea) {
          value = (el as HTMLTextAreaElement).value ?? "";
        } else if (el.hasAttribute("value")) {
          value = el.getAttribute("value") ?? "";
        }

        const disabled =
          (el as HTMLInputElement).disabled === true ||
          el.getAttribute("aria-disabled") === "true" ||
          el.hasAttribute("disabled");

        const name = el.getAttribute("name") || undefined;
        const id = el.getAttribute("id") || undefined;
        const ownerDoc = el.ownerDocument ?? doc;
        const label = getLabel(el, ownerDoc) || undefined;

        controls.push({
          frame: framePath,
          tag,
          type,
          name,
          id,
          label,
          value,
          checked,
          disabled: disabled ? true : undefined,
        });
      }

      if (el.shadowRoot) {
        traverse(el.shadowRoot, framePath, depth + 1);
      }

      if (tag === "iframe" || tag === "frame") {
        try {
          const subDoc = (el as HTMLIFrameElement).contentDocument;
          if (subDoc) {
            frameCounter++;
            const subName = `f${frameCounter}`;
            const nextPath = framePath === "top" ? subName : `${framePath} > ${subName}`;
            traverse(subDoc, nextPath, depth + 1);
          }
        } catch {
          // Cross-origin iframe
        }
      }
    }
  }

  traverse(doc, "top");
  return controls;
}
