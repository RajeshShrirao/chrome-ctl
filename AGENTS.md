# AGENTS.md

Repository guide for coding agents working on `chrome-ctl`.

## 1. Purpose & Architecture

`chrome-ctl` lets AI agents drive the user's real, signed-in Google Chrome profile by pairing a Go CLI and local daemon (`cmd/chrome-ctl`, `internal/*`) over localhost HTTP/WebSocket (`127.0.0.1:9229` default) with a Manifest V3 Chrome extension (`extension/src/{background,content,offscreen,popup,shared}`).

- **CLI (`internal/cli`, `internal/client`)**: Cobra commands calling the daemon via `POST /rpc` with the Bearer token stored in `~/.chrome-ctl/auth_token`.
- **Daemon (`internal/daemon`)**: Local HTTP/WebSocket server (`GET /health`, `POST /rpc`, `GET /ws`, `POST /pair`) that gates per-tab RPCs, runs daemon-local media/macro operations, and forwards browser RPCs to the extension.
- **Extension background & offscreen (`extension/src/{background,offscreen,popup}`)**: `offscreen/offscreen.ts` keeps the daemon WebSocket open across MV3 service-worker suspensions; `background/service_worker.ts` routes RPCs and drives trusted CDP input via `chrome.debugger`.
- **Extension content (`extension/src/content`)**: Bundled as an IIFE (`content/indexer.ts`) and injected into pages to walk top frames, open shadow roots, and same-origin iframes.

## 2. Key Directories & Files

### Go Entrypoint & CLI
- `cmd/chrome-ctl/main.go` — Binary entrypoint calling `cli.Execute()`.
- **`.gitignore` worktree gotcha**: `.gitignore` contains bare `chrome-ctl`, which ignores `cmd/chrome-ctl/`. Consequently, `cmd/chrome-ctl/` exists in the main checkout and is **absent in clean git worktrees**. Do not treat it as deleted or commit workarounds; `make build` and `make e2e` require a checkout that includes `cmd/chrome-ctl/`.
- `internal/cli/root.go` — Wires `NewRoot()` and `Execute()` for all Cobra subcommands.
- `internal/cli/batch.go` — Tokenizes step strings via `splitBatchArgs` (supporting quotes and backslash escapes), rejects nested `batch`, and runs each step on a fresh `NewRoot()` with `root.SetArgs(tokens)` and `root.ExecuteContext(...)` so Cobra does not fall back to `os.Args` and recurse.

### Go Daemon, Protocol & Support Packages
- `internal/daemon/server.go`, `auth.go`, `pair.go` — HTTP/WebSocket lifecycle, Bearer auth, and extension `/pair` handshake.
- `internal/daemon/rpc.go` — JSON-RPC dispatch (`handleLocalRPC`, `handleSessionStatus`, `forward`) and `--no-eval` enforcement.
- `internal/daemon/scheduler.go` — Per-tab FIFO reader/writer `tabGate` (`schedulerQueueLimit = 128`), `200ms` `singleFlight` read cache for `page.snap`/`page.read` (cleared on mutations), and `30s` `idempotencyTTL` for macro retries.
- `internal/daemon/media.go` — Daemon-local `media.shot`, `media.rec_start`, and `media.rec_stop` handlers backed by `internal/media`.
- `internal/daemon/macro.go` — Macro recording (`macroBeforeForward`, `macroAfterForward`, `buildMacroStep`) and saga playback (`handleMacroRun`).
- `internal/protocol/contract.json` — **Single source of truth** for `ProtocolVersion`, RPC method names, error codes, capabilities, and timeout budgets.
- `internal/protocol/messages.go` — Holds the `//go:generate` directive; run `go generate ./internal/protocol` to regenerate:
  - `internal/protocol/methods_generated.go`
  - `extension/src/shared/protocol.generated.ts`
  - `docs/PROTOCOL_CONTRACT.md`
- `internal/protocol/codegen_test.go` & `mirror_test.go` — Guard generated contract freshness (`TestGeneratedContractUpToDate`), timeout budgets (`TestContractTimeoutBudgets`), and TypeScript re-exports (`TestExtensionProtocolReExportsGeneratedContract`).
- `internal/config/` (`paths.go`, `token.go`) — Resolves `CHROME_CTL_HOME` (default `~/.chrome-ctl`), `CHROME_CTL_ADDR` (default `127.0.0.1:9229`), and 64-hex `auth_token`.
- `internal/client/client.go` — Authenticated HTTP/JSON-RPC client used by the CLI.
- `internal/bulk/bulk.go` & `internal/buildinfo/buildinfo.go` — Bulk operation helpers and build/version metadata.
- `internal/macro/` (`macro.go`, `saga.go`, `saga_step.go`, `journal.go`, `breaker.go`, `effect.go`) — Macro storage, templating, circuit breakers, and saga execution.
- `internal/media/` (`image.go`, `som.go`, `video.go`, `grid.go`) — WebP screenshot encoding, Set-of-Mark badges, grid compositing, and video helpers.

### Extension (`extension/src/`)
- `background/service_worker.ts` — RPC router, tab/window resolution, and `buildSnapResult`.
- `background/snap_format.ts` — `formatSnapHeaderUrl` (80-char query+hash truncation) and `formatSnapDiff` (diff formatting).
- `background/humanizer.ts` — Deterministic seedable PRNG and mouse/typing/scroll timing planners.
- `background/actions.ts` — Trusted CDP input dispatch for click, type, press, scroll, and upload.
- `background/debugger.ts` — `chrome.debugger` attach cache and one-shot auto-reattach on detach.
- `background/netlog.ts` — `NetLog` and `QuietTracker` backing `page.net` and `wait --network`.
- `background/resolver.ts` — Multi-tier element resolution for macro replay (`page.resolve`).
- `background/capture.ts`, `sso.ts`, `windows.ts` — Screenshot capture, SSO polling, and automation window tracking.
- `background/verify.ts`, `faults.ts` — Readback classification (`classifyReadBack`) and typed `RpcFault` errors.
- `content/dom.ts` — DOM walker and snapshot line builder (`MAX_SNAP_ELEMENTS = 400`).
- `content/identity.ts` — Deterministic fingerprinting, stable IDs, and `added`/`same`/`updated`/`moved` classification.
- `content/indexer.ts` — Content-script entrypoint maintaining per-document snapshot state and IDs.
- `content/read.ts` — Markdown, table, and link extraction (`page.read`).
- `content/forms.ts` — Form-control state extraction (`page.formstate`).
- `content/describe.ts`, `interact.ts` — Tiered element descriptors/resolution and wait/focus/metrics helpers.
- `shared/protocol.generated.ts`, `protocol.ts` — Generated contract constants and extension wire types/re-exports.
- `shared/content.ts`, `messages.ts` — Content-script and runtime message channel schemas.
- `offscreen/` (`offscreen.ts`, `offscreen.html`) — WebSocket keepalive relay to the daemon.
- `popup/` (`popup.ts`, `popup.html`, `popup.css`) — Extension status and pairing popup UI.

### Usage Skill, Docs & Tests
- `SKILL.md` + `references/` (`bulk-and-macros.md`, `forms-and-frames.md`, `visuals-and-sso.md`) — Usage skill for agents *operating* `chrome-ctl`.
  - Packaged into `dist/` by `make dist` and symlinked in `~/.gemini/config/skills/chrome-ctl`, `~/.agents/skills/chrome-ctl`, `~/.claude/skills/chrome-ctl`, and `~/.codex/skills/chrome-ctl`.
- `docs/` — Architecture and contract specs (`PROTOCOL_CONTRACT.md`, `TECH_SPEC.md`, `MV3_LIFECYCLE_SPEC.md`, `MACRO_SCHEMA.md`, `HUMANIZER_SPEC.md`, `MEDIA_PIPELINE.md`, `ALGORITHMS_AND_PRINCIPLES.md`).
- `tests/e2e/` (`fake_extension.mjs`, `run.sh`) — End-to-end harness driving a real daemon with a simulated WebSocket extension.
- `tests/fixtures/` (`infinite_scroll.html`, `modal.html`, `nested_iframes.html`, `shadow_dom.html`, `test_form.html`) — HTML test pages for extension/unit specs.
- `testdata/` (`data.json`, `other.html`, `sample.csv`, `spa.html`) — Sample fixtures for CLI/extension tests.

## 3. Build & Verification Commands

Always pass `-tags nodynamic` when invoking `go test` or `go build` directly (matching `GO_TAGS` in `Makefile`).

- `make build` — Compiles `bin/chrome-ctl` (`CGO_ENABLED=0 go build -tags nodynamic -trimpath -o bin/chrome-ctl ./cmd/chrome-ctl`).
- `make extension` — Installs deps if needed, then runs `npm --prefix extension run typecheck` (`tsc --noEmit`) and `npm --prefix extension run build` (`node build.mjs && node scripts/verify.mjs`).
- `make test` — Runs `extension`, `go test -tags nodynamic ./...`, and `npm --prefix extension test` (`node --test tests/*.test.ts`, 153 tests).
- `make e2e` — Runs `make build` and `bash tests/e2e/run.sh` against an isolated daemon on `127.0.0.1:9329` with `fake_extension.mjs`.
- `make dist` — Cross-compiles `dist/chrome-ctl-{darwin-arm64,darwin-amd64,linux-amd64}` and stages `dist/extension/`, `dist/SKILL.md`, and `dist/references/`.
- `make clean` — Removes `bin/` and `dist/` (keeps `extension/dist/` and `extension/node_modules/`).

Note: `make test` works in clean git worktrees; `make build` and `make e2e` require `cmd/chrome-ctl/main.go`.

## 4. Hard Invariants & Guardrails

- **Isolate live daemon & browser state**: Never touch the user's live daemon on `127.0.0.1:9229` or `~/.chrome-ctl` during tests. Always isolate `CHROME_CTL_HOME` and `CHROME_CTL_ADDR` (e.g. a temp dir and `127.0.0.1:9329`).
- **No automatic real-browser testing**: Do not launch or drive real Chrome during automated verification unless explicitly requested; verify with `make test` and `make e2e`.
- **Never hand-edit generated protocol files**: Update `internal/protocol/contract.json` and run `go generate ./internal/protocol`.
- **Keep flagless CLI outputs backward-compatible**: Preserve existing default output formats for flagless `open`, `scroll`, `press`, `snap`, and `formstate`.
- **Keep snapshot truncation bounds**: Keep `LABEL_MAX_CHARS = 80` and `VALUE_MAX_CHARS = 40` in `extension/src/content/dom.ts`.
- **Keep `"moved"` identity classification**: Keep `"moved"` in `extension/src/content/identity.ts`; collapse coordinate-only noise only when formatting diffs in `extension/src/background/snap_format.ts` / `service_worker.ts`.
- **Keep macro replay flag-clean**: Keep `internal/daemon/macro.go` (and `BuildAction` in `internal/macro/saga_step.go`) ignoring `--snap` and `--wait` during recording and replay.
- **Git & artifact hygiene**: Use Conventional Commits (`<type>(<scope>): <summary>`). Never stage `.env`, `auth_token`, `bin/`, `dist/`, or `extension/dist/`.
