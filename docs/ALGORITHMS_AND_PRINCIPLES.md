# Algorithms & Engineering Principles for chrome-ctl

Status: proposal · Scope: daemon, CLI, extension, tests · Date: 2026-11-09

chrome-ctl is a latency-sensitive, stateful control plane for a real browser. Two
properties matter more than raw feature count:

1. **Truthfulness** — the caller must know whether the page actually changed.
2. **Actionability** — an agent must be able to act without guessing which
   element it is looking at.

The proposals below are ordered by leverage, not novelty. Each one names the
current code it would replace, the algorithm, its complexity, and how to test it.

---

## Part I — Algorithms

### 1. Stable element identity + snapshot deltas

**Current behavior.** `snapshot()` in `extension/src/content/dom.ts` mints
monotonic ids, and `describeSnap()` in `indexer.ts` keeps a live `Map<id, Element>`.
Ids are never reused, so every `snap` forces the caller to rebuild its mental map.
`MacroTarget` tiers compensate for this at replay time, but interactive agents pay
the cost on every turn.

**Proposal.** Give every emitted element a deterministic *fingerprint*, match
fingerprints across snapshots, and emit a delta instead of a fresh list.

```
fingerprint(el) = hash64(
  role, accessibleName, testId, tag, type,
  stableAttrs(name, href-path, aria-controls),
  ancestrySignature(tags + nth-of-type, depth <= 8)
)
```

Matching algorithm per snapshot:

1. Bucket old elements by fingerprint.
2. Exact fingerprint + unique → same id.
3. Colliding fingerprints → greedy match by viewport distance, using a
   64 px grid so candidates are O(1) on average.
4. Unmatched → new id; old unmatched → `removed`.

Result shape: `{ id, change: "same" | "added" | "updated" | "moved" | "removed" }`.
`same` elements may omit geometry if it did not change.

**Complexity.** O(n) hashing + O(n + k) spatial matching; delta size O(changes).
**Invariants.** Ids stable for the life of a document; `docId` invalidates all;
ambiguous matches never silently retarget a macro step.
**Tests.** Property test: shuffling sibling order keeps ids for uniquely
identified elements. Renaming an aria-label still resolves through the tier
fallback. Golden delta fixtures for a modal open/close.

---

### 2. Single-pass snapshot with deferred layout reads

**Current behavior.** `collectCandidates()` recursively calls
`querySelectorAll("*")`, checks frame visibility with
`getBoundingClientRect()`, then `isCulled()` calls `getComputedStyle()` and
`getBoundingClientRect()` again. `hasEmittedAncestor()` walks up to 512 ancestors
per candidate (`dom.ts`). That is O(N·D) traversal plus repeated forced layout.

**Proposal.** Three phases with one pass each:

1. **Collect** — DFS with an explicit `suppressed` flag on the stack. When a
   node is emitted and its role is not child-addressable, mark its subtree
   suppressed. This replaces the per-element ancestor walk.
2. **Measure** — read `getComputedStyle()` and `getBoundingClientRect()` exactly
   once per surviving candidate, in document order, and memoize per element for
   the lifetime of the snapshot. Prefer `Element.checkVisibility()` when
   available.
3. **Cull** — apply viewport, modal, and hit-test filters using the cached
   measurements only.

```
walk(node, suppressed):
  if suppressed and not childAddressable(node): return
  if candidate(node): emit(node); suppressed = suppressesDescendants(node)
  for child in composedChildren(node): walk(child, suppressed)
```

**Complexity.** O(N) instead of O(N·D); one layout flush per phase instead of
one per candidate.
**Tests.** Benchmark with a 5k-node fixture; assert emitted ⊆ candidates, ids
unique, `omitted` equals the number dropped by the cap.

---

### 3. Multi-signal resolver with a precomputed candidate index

**Current behavior.** `findTargetOnce()` in `describe.ts` tries testId → aria →
text → XPath → CSS. Each tier calls `queryDeepAll()`, which rebuilds every deep
root (`deepRoots()` walks all shadow roots and same-origin frames). `isUsable()`
also calls `scrollIntoView()` during matching, so resolution has side effects.
First match wins, with no confidence signal.

**Proposal.** Separate *matching* from *revealing* and score all signals in one
pass.

```
S(c) = 0.45·sim_testid(c) + 0.25·sim_aria(c) + 0.15·sim_text(c)
     + 0.08·sim_role(c)  + 0.07·sim_css(c)
```

- Build the candidate set once per poll from the union selector.
- `sim` = 1.0 exact, 0.9 normalized, token-Dice (or Jaccard) otherwise.
- Hard filters: connected, visible, enabled, frame path resolves.
- Accept at `S >= 0.6`; when the top two are within 0.05, prefer exact testId,
  else DOM order, and return `ambiguous: true` plus the runner-up.
- `scrollIntoView()` moves to a separate `reveal()` called only after a match.

**Complexity.** O(C) per poll instead of O(5C + root rebuilds), and no layout
writes during matching.
**Tests.** Table-driven cases for label drift (`"Save changes"` vs `"Save"`),
duplicate buttons (unique testId wins), hidden decoys, and cross-frame targets.

---

### 4. Event-driven network quiet with hysteresis

**Current behavior.** `waitForQuiet()` in `netlog.ts` polls every 100 ms and
treats any request older than 2 s as a long poll. `QuietTracker` stores only a
count and a timestamp, so it cannot explain *why* a page is still busy.

**Proposal.** Make quiet a state machine driven by CDP events, with waiters
notified instead of polled.

```
IDLE --requestWillBeSent--> ACTIVE
ACTIVE --loadingFinished/Failed, inflight=0--> SETTLING(quietMs)
SETTLING --new request--> ACTIVE
SETTLING --timer fires--> QUIET (notify waiters)
```

- Coalesce one debounce timer per tab; waiters register a resolver.
- Classify traffic: ignore `websocket`/`eventsource`; weight requests by
  `type` and `initiator`; optionally ignore analytics/beacon paths.
- Track an epoch per navigation; completions from an older epoch are ignored.
- Return a reason: `quiet`, `timeout`, `no-traffic`, `tab-gone`.

**Complexity.** O(1) per event; no polling loop and no 100 ms latency floor.
**Tests.** Fake clock: request at t=0, quiet at t=quietMs; late completion from a
previous navigation must not extend the wait; long-poll exclusion is explicit.

---

### 5. Bulk crawler: AIMD, full-jitter retry, and near-duplicate detection

**Current behavior.** `runBulk()` in `cli/bulk.go` is strictly sequential, uses a
fixed jitter range, and stops after N *exact* SHA-256 repeats. A 429 or 503 is
recorded as a failure with no retry.

**Proposal.** A small scheduler with per-host fairness:

1. **Rate control** — token bucket per host; AIMD: `r += α` on success,
   `r *= 0.5` on 429/5xx, clamped to `[rmin, rmax]`.
2. **Retry** — only transient classes (408, 425, 429, 5xx, network). Delay
   `min(cap, base·2^n)` with full jitter `rand(0, delay)`, honoring `Retry-After`.
3. **Circuit breaker** — after k consecutive failures for a host, open for a
   cooldown, then a single half-open probe.
4. **Near-duplicates** — 64-bit SimHash over word shingles, indexed by 4×16-bit
   LSH bands; Hamming distance ≤ 3 is a near-duplicate. Keep the exact hash as a
   fast path.
5. **Durability** — append-only manifest with periodic fsync; markdown written
   to a temp file and atomically renamed.

**Complexity.** O(1) scheduling per item; near-duplicate lookup O(1) average.
**Tests.** Fixture server that returns 429 then 200; assert the retry delay is
inside the jitter bounds. Property test: SimHash is stable under whitespace and
punctuation changes but differs for unrelated pages.

---

### 6. Deterministic procedural humanization

**Current behavior.** `humanizer.ts` calls `Math.random()` directly, keeps a
package-global Box-Muller spare, and uses i.i.d. Gaussian tremor. Results cannot
be replayed from a seed, which makes flaky behavior hard to reproduce.

**Proposal.** Inject randomness and model motion more faithfully:

- `interface Rng { next(): number; gaussian(): number }`; default a
  crypto-seeded xoshiro128**; per-action seed returned in the result for replay.
- Replace the sigmoid time warp with a **minimum-jerk** profile
  `s(τ) = 10τ³ − 15τ⁴ + 6τ⁵`, still scaled by a Fitts's-law duration.
- Replace white-noise tremor with a mean-reverting **Ornstein-Uhlenbeck**
  process, and add a small overshoot + correction for far targets.
- Typing: segment by grapheme cluster (`Intl.Segmenter`), sample flight times
  from a log-normal, and use `Input.insertText` for characters with no key code.
- Validate distributions with a two-sample KS test in tests.

**Complexity.** Unchanged O(n) in path length; adds O(1) state per action.
**Note.** Keep this behind an explicit capability flag and document that it is
for reliability/QA, subject to the target site's terms.

---

### 7. Macro replay as a saga with effect signatures

**Current behavior.** `runBatch()` and macro replay stop on the first error and
never retry. An `ErrDispatchedUnconfirmed` timeout is ambiguous: the action may
or may not have happened.

**Proposal.** Give each step a contract:

```
step = {
  precondition, action, postcondition,
  retry: { max, base, cap, retryOn },
  timeout, idempotencyKey, compensate
}
```

- Classify errors: **transient** (connection, unconfirmed), **permanent**
  (not found, not interactable), **ambiguous** (timeout after dispatch).
- Retry only transient errors with full jitter. For ambiguous errors, read the
  **effect signature** (URL, value, checked, disabled, target count) before
  deciding to retry.
- Checkpoint after each step to JSONL and resume by step id.
- Run `compensate` in reverse order for irreversible partial effects.
- Trip a per-step circuit breaker and write a dead-letter record with a snapshot.

**Complexity.** O(steps); no change to the happy path.
**Tests.** Simulated timeout where the effect landed: must verify, not retry.
Timeout where it did not land: exactly one retry. Compensation runs in reverse.

---

### 8. Per-tab scheduler and single-flight in the daemon

**Current behavior.** `daemon.forward()` multiplexes all methods over one
extension connection. Two CLI processes can interleave mutating actions on the
same tab, and concurrent `page.snap` calls each do full work.

**Proposal.** Schedule by `tabId`:

- Mutating methods (`action.*`, `tab.open`, navigation) → FIFO per tab.
- Reads may run concurrently with reads, never across a mutation boundary.
- **Single-flight** `page.snap` / `page.read` keyed by `(tabId, paramsHash)` with
  a 100–250 ms TTL; share the result.
- Bounded queue with `ErrBusy (-32012)` when full; round-robin across tabs for
  fairness.
- Accept an idempotency key and dedupe retries within a short window.

**Complexity.** O(1) enqueue with a list per tab; no global lock on the hot path.
**Tests.** Race test with `-race`: two concurrent clicks on one tab never
interleave; two snaps within the TTL share one extension round-trip.

---

### 9. Token-budgeted snapshot rendering

**Current behavior.** `formatLine()` emits every element up to
`MAX_SNAP_ELEMENTS = 400`, in DOM order.

**Proposal.** Score and select under a character budget:

```
score = 3·interactive + 3·changed + 2·inViewport + 1·formField + 1·hasName
```

Greedy selection is O(n log n) and deterministic; reserve slots for changed
elements, then visible interactive controls, then the rest. Always report
`omitted` and a short hint (`"12 more below the fold; use snap --all"`).

**Tests.** Golden output for a dense grid; invariant: selection is deterministic
for identical input and the budget is never exceeded.

---

### 10. One contract, generated twice

**Current behavior.** `internal/protocol/messages.go` and
`extension/src/shared/protocol.ts` duplicate method names, error codes, and
timeout tables. They can drift silently.

**Proposal.** Treat a machine-readable schema (JSON Schema or Go structs) as the
single source of truth and generate the TypeScript types, error constants, and
docs. Add a CI check that fails when generated output differs from the committed
files. Add `protocolVersion` and capability negotiation to `system.ready`.

**Tests.** Round-trip fixtures in both languages; a contract test that every
`Methods` entry exists on both sides.

---

## Part II — Engineering principles

| # | Principle | Application in this repo |
|---|-----------|--------------------------|
| 1 | **Pure core, imperative shell** | Keep `dom.ts`, `humanizer.ts`, `describe.ts` free of `chrome.*`; inject clock/RNG/DOM so they stay jsdom-testable. Already the pattern — protect it. |
| 2 | **Make illegal states unrepresentable** | Replace bool soup in `PageWaitParams` (`Idle`, `Network`, `Text`, `Selector`) with a tagged mode. A wait cannot be both network and selector by construction. |
| 3 | **Errors are data** | Keep stable `-320xx` codes; wrap with `%w`; never branch on message text. Map errors once at each boundary (extension → daemon → CLI). |
| 4 | **Inject time, randomness, I/O** | Every `time.Now()`, `Date.now()`, `Math.random()`, and `crypto/rand` should be replaceable. This is the cheapest route to reproducible flake reports. |
| 5 | **Every blocking call has a deadline** | `client.Client` uses a bare `http.Client{}`; add `Timeout`, tuned `Transport` (keep-alive, `MaxIdleConnsPerHost`), and require a context at the call site. |
| 6 | **Bound everything** | Element caps, net ring size, request body size, queue depth, retries, recursion depth. Prefer an explicit `ErrBusy` over unbounded memory. |
| 7 | **Idempotency and resumability** | The bulk manifest is the right instinct. Extend it: atomic append, fsync cadence, temp-file + rename, resume by content hash as well as URL. |
| 8 | **Context end-to-end** | Propagate cancellation from Cobra through RPC, daemon, and extension; drop queued work when the caller is gone. |
| 9 | **Observability by default** | Structured logs with `requestId` and `tabId`; latency histograms for snap/resolve/click; counters for pending depth, timeouts, and retries. Use a t-digest/HDR histogram for p95/p99. |
| 10 | **Complexity discipline** | Record the big-O of hot paths in comments. Any new full-DOM scan or per-element ancestor walk needs a benchmark and a cap. |
| 11 | **Test the edges, not the middle** | Table-driven tests, fuzz `parseWaitArg`/`bulkSlug`/protocol decoding, property tests for snapshot invariants, `go test -race` and `-count=1` in CI, golden tests for rendered snapshots. |
| 12 | **Security is layered** | Token comparison should be constant-time; prefer the `Authorization` header over `?token=`; add an Origin allowlist for `/ws` even with auth; rotate tokens; keep eval disabled by default. |
| 13 | **Small, reversible changes** | Conventional commits are already used. Pair each algorithm change with a benchmark or a golden fixture so regressions are visible. |

### Two rules that prevent most regressions

- **No side effects during matching.** Resolution must be read-only; scrolling,
  focusing, and clicking happen only after a target is chosen.
- **No unbounded per-element work.** If a loop touches every node, it gets a cap,
  a benchmark, and a comment explaining the bound.

---

## Part III — Suggested sequence

| Priority | Change | Why first | Effort |
|----------|--------|-----------|--------|
| P0 | Single-pass snapshot + deferred layout (#2) | Largest CPU/layout win; no protocol change | S–M |
| P0 | Precomputed resolver + remove scroll side effect (#3) | Fewer round-trips, more reliable targeting | M |
| P0 | Event-driven quiet (#4) | Removes the 100 ms floor and the long-poll heuristic | M |
| P0 | HTTP client timeouts + transport tuning (#5, principle 5) | One-line-class fix with outsized reliability gain | S |
| P1 | Stable ids + deltas (#1) | Biggest agent-UX improvement; enables cheap diffing | M–L |
| P1 | Bulk AIMD + SimHash (#5) | Directly improves crawl success and dedupe | M |
| P1 | Per-tab scheduler + single-flight (#8) | Prevents interleaving bugs under concurrency | M |
| P2 | Macro saga + effect signatures (#7) | Safer replay, better failure messages | L |
| P2 | Seeded humanizer (#6) | Reproducible behavior and statistical tests | M |
| P2 | Protocol codegen (#10) | Prevents long-term drift | S–M |

## Part IV — Acceptance metrics

Track these before and after:

- `snap` p50/p95 latency and elements/second on 1k and 5k-node fixtures.
- Resolver attempts per successful action and `ambiguous` rate.
- `wait --network` p50/p95 and false-quiet rate (quiet declared while a
  same-origin document request was still in flight).
- Bulk success rate per host, retries per item, and near-duplicate hit rate.
- Flake rate of the macro e2e suite; time to reproduce a failed run from its seed.
- Race-detector clean runs and coverage of `internal/protocol` and `internal/daemon`.

**Definition of done for any item above:** algorithm documented, bounded,
benchmarked (or golden-tested), and observable in logs/metrics.
