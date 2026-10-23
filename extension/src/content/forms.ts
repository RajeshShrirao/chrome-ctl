/** Form-control helpers shared by the snapshot indexer (Phase 2 `--forms`). */

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
