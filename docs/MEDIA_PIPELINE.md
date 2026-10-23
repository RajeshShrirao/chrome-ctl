# Media & Vision Token Optimization Pipeline Spec

**Document Version:** 1.0  
**Target Subsystem:** `internal/media` & `extension/src/content/som.ts`  
**Target Format:** WebP (Images/Grids) & WebM VP9 (Video)  

---

## 1. Vision Token Economics & Problem Statement

Vision-capable LLMs (GPT-4o, Claude 3.5 Sonnet, Gemini 1.5/2.0) bill image tokens based on tile subdivisions (typically $512 \times 512\text{px}$ tiles).

| Format | Resolution | File Size | Estimated Vision Tokens |
| :--- | :--- | :--- | :--- |
| **Standard Selenium/Playwright PNG** | $1920 \times 1080$ | $1.8\text{ MB}$ | **~1,600 tokens** |
| **Full Page PNG Dump** | $1920 \times 4800$ | $6.2\text{ MB}$ | **~7,200 tokens** |
| **Standard 30 FPS Video (10s)** | $1080\text{p}$ MP4 | $12\text{ MB}$ | **Unsupported or >25k tokens** |
| **`chrome-ctl shot` (WebP 65%)** | $1024 \times 576$ | **$38\text{ KB}$** | **~255 tokens (84% reduction)** |
| **`chrome-ctl shot --gray`** | $1024 \times 576$ | **$22\text{ KB}$** | **~170 tokens (89% reduction)** |
| **`chrome-ctl rec --grid` (15s)** | $1024 \times 768$ (12 frames) | **$54\text{ KB}$** | **~340 tokens (Entire workflow!)** |

---

## 2. Set-of-Mark (SoM) Visual Overlay (`--som`)

When web interfaces use non-semantic DOM elements (custom canvas buttons, icon-only toolbars, complex React portals), text snapshots can be ambiguous. Set-of-Mark draws numeric badges directly over the interactive elements.

```
+--------------------------------------------------------+
|  [1] Search...                      [2] Compose        |
|  +--------------------------------------------------+  |
|  | [3] Rahul Verma (Founder)                        |  |
|  |     "Hey, are you free to chat tomorrow?"        |  |
|  +--------------------------------------------------+  |
|  | [4] Type a reply...              [5] Attach  [6]Send|
+--------------------------------------------------------+
```

### 2.1 Badge Visual Specifications
To maximize OCR readability for Vision models while minimizing visual occlusion:
* **Background:** `rgba(15, 23, 42, 0.90)` (Tailwind Slate-900)
* **Text Color:** `#FACC15` (High-contrast Yellow-400)
* **Border:** `1px solid rgba(255, 255, 255, 0.8)`
* **Border Radius:** `4px`
* **Font:** `bold 11px / 1.1 "SF Mono", monospace`
* **Padding:** `2px 5px`
* **Z-Index:** `2147483647` (Max integer z-index)

### 2.2 Collision Avoidance & Micro-Clustering
When interactive elements are tightly packed (e.g., pagination buttons or social media reaction bars):
* Badges are positioned at the top-left corner of the element's bounding box: $(x, y)$.
* If two badges overlap by $>50\%$, the secondary badge offsets vertically by $+14\text{px}$ or collapses into a grouped notation.

### 2.3 Injection & Teardown Protocol
1. Extension receives `media.shot` with `som: true`.
2. Content script creates a temporary container `<div id="__chrome_ctl_som_layer">` in the top-level DOM.
3. Badges are rendered synchronously using pre-calculated bounding rects.
4. Service worker calls `chrome.tabs.captureVisibleTab(null, { format: 'png' })`.
5. Content script removes `__chrome_ctl_som_layer` within $16\text{ms}$ (1 frame) so the user experiences zero visual flicker.

---

## 3. Go Compression & Optimization Engine

Located in `internal/media/image.go`:

```
Raw Screen Buffer (PNG/JPEG)
       |
       v
[ Lanczos3 Resampling ] ---> Downscale to max-width: 1024px (preserves aspect ratio)
       |
       +---> [ Grayscale Transform ] (Optional --gray flag: Y = 0.299R + 0.587G + 0.114B)
       |
       v
[ WebP Encoder ] ----------> Quality: 65, Effort: 4
       |
       v
Compressed WebP Artifact (~35KB)
```

### 3.1 Grayscale Mode (`--gray`)
For document signing, form auditing, and text-heavy receipts, color information is redundant:
* Eliminates the $U$ and $V$ chrominance sub-bands.
* Reduces WebP file size by an additional $35\text{–}45\%$ without reducing OCR accuracy.

---

## 4. Contact Sheet Storyboard Recording (`rec --grid`)

Autonomous agents frequently need to verify asynchronous multi-step flows:
* Example: Click "Submit Application" $\rightarrow$ Wait for Loading Spinner $\rightarrow$ Receive Confirmation Banner.

Sending 15 separate screenshots or a 30 FPS video burns thousands of tokens. `chrome-ctl rec --grid` compiles a $3 \times 4$ contact sheet into a **single WebP image**.

### 4.1 Contact Sheet Architecture
```
+-------------------------------------------------------------------------+
| [00:01] Form Filled      | [00:02] Click Submit    | [00:04] Spinner Shown |
+--------------------------+-------------------------+-----------------------+
| [00:06] Processing Auth   | [00:08] Redirecting...  | [00:10] Success Page  |
+--------------------------+-------------------------+-----------------------+
| [00:12] Post Created     | [00:13] Feed Refreshed  | [00:15] Task Complete |
+-------------------------------------------------------------------------+
```

* **Frame Decimation:** Captures at $1\text{–}2\text{ FPS}$.
* **Tile Rescaling:** Each frame is scaled to $340 \times 190\text{px}$.
* **Timestamp Badging:** Injects high-contrast elapsed timestamp (`[00:04]`) in bottom-left corner of each tile.
* **Resulting Canvas:** $1024 \times 768\text{px}$ stitched image.
* **Token Cost:** Equals **one single image turn** (~340 tokens) to inspect a full 15-second operation.
