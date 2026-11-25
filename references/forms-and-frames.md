# Forms, Frames, and Escape Hatches

Detailed workflows for inspecting multi-frame forms, clicking across nested iframes or screenshot coordinates, handling controlled JS widgets, and using `describe`, `href`, and `eval`. See [../SKILL.md](../SKILL.md) for core lifecycle rules.

## Form state inspection (`formstate`)

`formstate` inspects every form control across the top document and all same-origin frames:

```bash
chrome-ctl formstate                        # controls across all same-origin frames (capped by default)
chrome-ctl formstate --frame f2             # filter controls inside frame f2 (or by frame name/selector)
chrome-ctl formstate --filled               # only populated inputs and checked radios/checkboxes
chrome-ctl formstate --max-controls 100     # adjust safe control cap
chrome-ctl formstate --full                 # emit every control without capping
chrome-ctl formstate --json                 # structured JSON array of FormControlState
```

- **Output format:** Each line prints `[<frame>]` (`[top]` or frame tag such as `[f1]`, `[f2]`), selection state for radios `(•)` / `( )` and checkboxes `[x]` / `[ ]`, `<tag:type>`, and present metadata: `name="..."`, `id="..."`, `value="..."`, `disabled`, `label="..."`.
- **Token-saving flags:** On large multi-step forms or admin panels, use `--filled` to audit only controls that have a value or are checked, `--frame <fN|name|selector>` to scope to one iframe, or `--max-controls <n>` / `--full` to tune or remove the default control cap.
- **Pre-submit verification recipe:** Before submitting any form, application, or assessment:
  1. Run `chrome-ctl read --forms` (renders inline options such as `(•) CPU`, `( ) GPU`, `[x] Oval`, `[ ] Rect`) and/or `chrome-ctl formstate --filled`.
  2. Use `chrome-ctl snap --forms` when you need actionable IDs for remaining empty inputs, selects, textareas, or submit controls.
  3. Confirm every choice matches intent before clicking Submit (and observe [Stop and ask](../SKILL.md#stop-and-ask) for graded assessments or destructive/spendable submits).

## Nested iframes and Shadow DOM

- **Empty-looking page heuristic:** If `snap` returns unexpectedly few elements on a rich page, assume content lives inside an iframe. Same-origin frames are readable by `read`; `snap`/`click` traversal can miss frames that are zero-sized or deeply nested unless `--frames` is passed.
- **Forcing frame traversal:** `chrome-ctl snap --frames` traverses all same-origin frames regardless of size or visibility. Elements inside frames are tagged with their frame path (e.g. `[41] (f2)`).
- **Automatic coordinate translation:** `click <id>` and `describe` automatically translate element coordinates across same-origin iframe boundaries into top-frame viewport CSS pixels.
- **Visibility limits:** Open shadow roots are traversed automatically, whereas **cross-origin iframes** and **closed shadow roots** remain invisible to script inspection.
- **Target escalation ladder** when `element not found` or `not interactable` occurs (see [Recover](../SKILL.md#recover)):
  `snap` → `snap --all` → `snap --deep` → `snap --frames` → `describe "css=<selector>"` → `press Tab` → `clickat X,Y`.

## Clickat and screenshot coordinates

`clickat` clicks an exact point in top-frame viewport CSS pixels (the coordinate space printed by `snap` and `describe`):

```bash
chrome-ctl clickat 65,238                             # comma-separated viewport CSS pixels
chrome-ctl clickat 65 238                             # space-separated viewport CSS pixels
chrome-ctl clickat --from-shot /tmp/view.webp 100 200 # convert screenshot pixels -> viewport pixels
chrome-ctl clickat 65,238 --double --snap             # double-click + compact DIFF snapshot
```

- **Nested frame `frame-clipped` / obscured workaround:** If `click <id>` refuses an element inside a nested frame (for example hit-testing reports `frame-clipped` or container obscuring), accumulate `getBoundingClientRect` offsets down the frame chain with `eval`, then issue `clickat X,Y`.
- **Why `--from-shot` matters:** [Screenshots](visuals-and-sso.md#screenshots) are downscaled by `--max-width` (default `1024`, so e.g. a `1414x867` viewport becomes a `1024x628` image at scale `0.724`). Passing raw screenshot coordinates to `clickat` without `--from-shot` misses the target. When `--from-shot <path>` is passed, `clickat` reads the sibling `<path>.meta.json` sidecar (`viewportWidth`, `viewportHeight`, `imageWidth`, `imageHeight`, `scale`) and converts image pixels to viewport CSS pixels automatically.
- **Determinism & natural motion:** Clicks follow a minimum-jerk cursor path with mean-reverting tremor and slight overshoot only on far targets (20–35ms press/release, keeping short moves brisk). Every `click`, `clickat`, `type`, `press`, and `scroll` result emits a `seed`; pass `--seed <str>` to replay the exact landing point, path, and cadence. Seeds are for reliability and QA, never for evading a site's controls.

## JS widgets and controlled inputs

- **Controlled React/Vue inputs:** Controlled components may ignore synthetic input events until focused, or overwrite programmatic DOM mutations on blur/render.
  - Trust `type` read-back lines (see [Verify](../SKILL.md#verify)): `(verified; …)` confirms the DOM value matched; `WARN: Typed N chars into [id] but the field reads empty` means the site rejected it (readonly or controlled); `WARN: … but the field reads "…" instead` means wrong target or autofill conflict. Exit code stays `0`, so treat `WARN` as unverified: `snap`, `click` the field directly to focus, check target, and retry.
- **Avoid `eval element.click()` on JS widgets:** On JS-heavy widgets (e.g., D2L/Brightspace LMS controls, custom comboboxes, rich dropdowns), synthetic `.click()` calls via `eval` often skip framework change/save listeners. Prefer `click <id>` or `clickat` (trusted CDP input events).
- **Collapsed controls & `<details>`:** `collapsed`/`expanded` state comes from `aria-expanded` or `<details>` summary. A closed combobox appears in `snap`, while its listbox options do not — `click` the combobox (optionally with `--snap`) so options enter the DOM, then act on the new IDs.
- **Hidden file inputs:** Hidden `<input type="file">` controls are emitted at their visible `<label>` with state `file`. Always use `chrome-ctl upload <id> <file_path>`; never click the hidden input directly.
- **Keyboard fallback:** When pointer heuristics cannot reach a control, use `chrome-ctl press Tab` until focused, then `chrome-ctl press Enter` or `chrome-ctl press Space`.

## Escape hatches (`describe`, `href`, `scroll --to`, `eval`)

`url`, `describe`, `clickat`, and `eval` are deliberate escape hatches — not the default path.

```bash
chrome-ctl describe 12
chrome-ctl describe 12 --frame f2
chrome-ctl describe "css=div[role=combobox]" --frame f1
chrome-ctl href 12                          # resolved absolute URL only
chrome-ctl href 12 --frame f2
chrome-ctl click 12 --timeout 10000         # timing breakdown: dispatch + DOM settle
chrome-ctl scroll down 0.75 --snap          # 5s timeout; targets innermost scrollable frame
chrome-ctl scroll --to "div.quiz-summary"   # scroll directly to CSS selector
chrome-ctl eval 'document.title'
chrome-ctl eval --raw 'location.href'
chrome-ctl eval --await 'fetch("/api/status").then(r => r.json())' --timeout 8000
```

- **`describe <id|css=selector> [--frame <fN|name|selector>]`:** Prints `<tag> "label" @x,y WxH` plus `role`, `type`, `id`, `name`, `href`/`src`, `value`, `state`, `matches` count (when selector matches multiple elements), and every attribute.
- **`href <id|css=selector> [--frame <fN|name|selector>]`:** Prints only the resolved absolute URL (fails if the element has no `href`).
- **`scroll`:** `scroll <down|up> [amount]` defaults to `0.8` viewport fractions (range `(0, 10]`, 5s RPC timeout) and targets the innermost scrollable frame. `scroll --to "<selector>"` jumps directly to an element; `--snap` attaches a compact `DIFF` snapshot.
- **`eval "<expr>" [--raw] [--await] [--timeout <ms>]`:**
  - Runs JavaScript in the page's main world over CDP (`Runtime.evaluate`), so it works even when page CSP blocks injected scripts.
  - Returns JSON-serialized output (`--raw` unquotes string results); `--await` waits for a returned Promise; `--timeout` accepts `250–30000` ms (default `5000`).
  - Output is truncated at `100,000` characters, and every expression is audit-logged to the extension console.
  - **Safety rule:** `eval` is instant, skips humanisation, and can read anything the page can read. Never use it to bypass a confirmation or scrape beyond task scope.
  - **Disabling `eval`:** Run `chrome-ctl daemon --stop && chrome-ctl daemon --start --no-eval`. The `--no-eval` gate survives background daemon re-exec, and `eval` returns `feature disabled` (ask the user before restarting without `--no-eval`).
