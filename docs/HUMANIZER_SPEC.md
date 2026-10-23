# Humanizer Engine Specification: Biometrics & Anti-Bot Evasion

**Document Version:** 1.0  
**Target Subsystem:** `internal/humanizer` & `extension/src/background/debugger.ts`  
**Compliance Standard:** Full W3C UI Events & Chrome DevTools Protocol (`Input.dispatchMouseEvent`, `Input.dispatchKeyEvent`)  

---

## 1. Threat Model & Detection Surface

Modern anti-bot behavioral systems (DataDome, Cloudflare Turnstile, Akamai Bot Manager, LinkedIn BotGuard, Meta Biometrics) evaluate browser automation along two vectors:

1. **Synthetic Event Fingerprinting:**
   * Synthetic JavaScript events (`element.click()`, `new MouseEvent(...)`) produce `event.isTrusted = false`.
   * Automation tools like Selenium or basic Puppeteer leave detectable `navigator.webdriver = true` flags and zero CDP event signatures.
2. **Behavioral Biometrics & Kinematics:**
   * **Linear Trajectories:** Mouse paths moving in straight lines with constant velocity $v = \text{const}$.
   * **Instantaneous Teleportation:** Mouse coordinate jumping from $(x_1, y_1)$ to $(x_2, y_2)$ in $0\text{ms}$.
   * **Dead-Center Targeting:** Repeatedly clicking the exact center coordinates $(\frac{w}{2}, \frac{h}{2})$ of elements.
   * **Isochronous Typing:** Uniform inter-keystroke intervals (e.g., exactly $50\text{ms}$ between every key).
   * **Instantaneous Scrolling:** Discontinuous `window.scrollTo()` jumps without intermediate wheel ticks or reading pauses.

---

## 2. Mathematical Kinematics & Mouse Trajectory

All cursor movement commands in `chrome-ctl` execute via cubic Bézier curves augmented by Fitts's Law and Gaussian micro-jitter.

### 2.1 Cubic Bézier Curve Formulation
The trajectory between starting point $P_0 = (x_0, y_0)$ and target $P_3 = (x_3, y_3)$ is parameterized by $t \in [0, 1]$:

$$B(t) = (1-t)^3 P_0 + 3(1-t)^2 t P_1 + 3(1-t) t^2 P_2 + t^3 P_3$$

#### Control Point Generation ($P_1, P_2$)
To simulate the human wrist-and-forearm biomechanical arc:
1. Compute the Euclidean distance $D = \|P_3 - P_0\|_2$ and angle $\theta = \operatorname{atan2}(y_3 - y_0, x_3 - x_0)$.
2. Calculate perpendicular unit vector $\vec{n} = (-\sin\theta, \cos\theta)$.
3. Sample curvature deviations $c_1, c_2 \sim \mathcal{N}(0, 0.25 D)$.
4. Position control points along the longitudinal axis:
   $$P_1 = P_0 + \frac{1}{3}(P_3 - P_0) + c_1 \vec{n}$$
   $$P_2 = P_0 + \frac{2}{3}(P_3 - P_0) + c_2 \vec{n}$$

### 2.2 Velocity Profile (Fitts's Law & Minimum Jerk)
Humans accelerate quickly from rest and decelerate as the cursor converges on small targets:
* The time parameter $t \in [0, 1]$ is warped by a sigmoid easing function:
  $$S(\tau) = \frac{1}{1 + e^{-k(\tau - 0.5)}}, \quad \tau \in [0, 1]$$
* Total travel time $T$ scales with target distance $D$ and target width $W$:
  $$T = a + b \log_2\left(1 + \frac{D}{W}\right) + \epsilon$$
  where $a \approx 120\text{ms}$, $b \approx 80\text{ms}$, and $\epsilon \sim \mathcal{N}(0, 25\text{ms})$.

### 2.3 Micro-Jitter (Neuromuscular Noise)
Real human hands produce physiological tremor ($8\text{–}12\text{ Hz}$):
At each step $i$ along the trajectory, add orthogonal perturbation:
$$P'(t_i) = B(t_i) + \delta_i, \quad \delta_i \sim \mathcal{N}(0, \sigma^2), \quad \sigma = 0.8\text{px}$$

### 2.4 Gaussian Click Landing Distribution
Clicks must never land on the exact bounding box center $(x_c, y_c)$. Target landing $(x_L, y_L)$ is generated using a 2D Gaussian distribution clipped inside the element's interior padding:
$$x_L = x_c + \Delta x, \quad \Delta x \sim \mathcal{N}\left(0, \left(\frac{w}{6}\right)^2\right)$$
$$y_L = y_c + \Delta y, \quad \Delta y \sim \mathcal{N}\left(0, \left(\frac{h}{6}\right)^2\right)$$
$$\text{clamped to } [x_{\min} + 4, x_{\max} - 4] \times [y_{\min} + 4, y_{\max} - 4]$$

---

## 3. Cadence Typing Engine

Dispatches `Input.dispatchKeyEvent` with natural rhythm:

```
[Key Down] --(hold time: 45-85ms)--> [Key Up] --(flight time: 40-160ms)--> [Next Key]
```

### 3.1 Timing Distributions
* **Base Flight Time:** Sampled from a log-normal distribution:
  $$\Delta t_{\text{flight}} \sim \operatorname{LogNormal}(\mu = 4.2, \sigma = 0.3) \implies \text{Median} \approx 66\text{ms}$$
* **Bigram Acceleration:** When typing common English letter pairs (`th`, `he`, `in`, `er`, `an`, `re`, `on`, `at`), the flight time is reduced by $35\%$.
* **Punctuation Pauses:** Characters `.` `,` `!` `?` `:` `;` inject a cognitive pause of $250\text{ms}–550\text{ms}$.
* **Word Boundaries:** Space characters `' '` inject a cognitive pause of $120\text{ms}–280\text{ms}$.

### 3.2 Human Typo Simulation (`--typo-rate <float>`)
If enabled (default `0.02`, or 2% chance per keystroke):
1. Look up QWERTY keyboard adjacent keys for the target character (e.g., `'o'` adjacent to `'i'`, `'p'`, `'k'`, `'l'`).
2. Dispatch `keyDown` + `keyUp` for the erroneous adjacent key.
3. Pause for $140\text{ms}–260\text{ms}$ (perceptual realization delay).
4. Dispatch `key: "Backspace"`.
5. Pause for $90\text{ms}–180\text{ms}$.
6. Dispatch `keyDown` + `keyUp` for the intended character.

---

## 4. Natural Momentum Scrolling

Replaces instant `window.scrollTo` with progressive mouse wheel ticks via CDP:

* **Wheel Tick Distribution:** Dispatches $N$ sequential `Input.dispatchMouseEvent` with `type: mouseWheel`.
* **Momentum Decay:** Initial delta $\Delta y_0$ decays exponentially:
  $$\Delta y_{k+1} = \Delta y_k \times \lambda, \quad \lambda \approx 0.88$$
* **Reading Gaze Pauses:** After scrolling $600\text{–}900\text{px}$, insert a random pause ($1.2\text{s}–3.5\text{s}$) simulating content reading.

---

## 5. Chrome DevTools Protocol (CDP) Dispatch Implementation

```typescript
// extension/src/background/humanizer.ts

export async function dispatchHumanClick(
  tabId: number,
  startX: number,
  startY: number,
  targetRect: DOMRect
): Promise<{ durationMs: number; finalX: number; finalY: number }> {
  const startTime = Date.now();
  const targetX = targetRect.left + targetRect.width / 2 + randomGaussian(0, targetRect.width / 6);
  const targetY = targetRect.top + targetRect.height / 2 + randomGaussian(0, targetRect.height / 6);

  const points = generateBezierPoints(startX, startY, targetX, targetY);

  // 1. Move cursor along Bezier curve
  for (const pt of points) {
    await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
      type: 'mouseMoved',
      x: Math.round(pt.x),
      y: Math.round(pt.y),
    });
    await sleep(pt.delayMs);
  }

  // 2. Pre-click hesitation (30-80ms)
  await sleep(randomInt(30, 80));

  // 3. Mouse Down
  await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
    type: 'mousePressed',
    x: Math.round(targetX),
    y: Math.round(targetY),
    button: 'left',
    clickCount: 1,
  });

  // 4. Click Hold Duration (45-95ms)
  await sleep(randomInt(45, 95));

  // 5. Mouse Up
  await chrome.debugger.sendCommand({ tabId }, 'Input.dispatchMouseEvent', {
    type: 'mouseReleased',
    x: Math.round(targetX),
    y: Math.round(targetY),
    button: 'left',
    clickCount: 1,
  });

  return {
    durationMs: Date.now() - startTime,
    finalX: Math.round(targetX),
    finalY: Math.round(targetY),
  };
}
```
