import test from "node:test";
import assert from "node:assert/strict";

import {
  DEFAULT_LONG_POLL_MS,
  MAX_NET_ENTRIES,
  NetLog,
  QuietTracker,
} from "../src/background/netlog.ts";

test("quiet tracker requires zero inflight and a full quiet window", () => {
  const tracker = new QuietTracker(1_000);
  assert.equal(tracker.isQuiet(1_000, 500), false);
  assert.equal(tracker.isQuiet(1_499, 500), false);
  assert.equal(tracker.isQuiet(1_500, 500), true);

  tracker.setInflight(2);
  tracker.noteActivity(2_000);
  assert.equal(tracker.isQuiet(10_000, 500), false);

  tracker.setInflight(0);
  assert.equal(tracker.isQuiet(2_499, 500), false);
  assert.equal(tracker.isQuiet(2_500, 500), true);
});

test("net log records status and duration for a completed request", () => {
  const log = new NetLog();
  log.requestStart("r1", "https://example.test/a.js", "GET", "Script", 1_000);
  log.response("r1", 200, 1_100);
  assert.equal(log.inflight, 1);
  log.finish("r1", 1_250);

  assert.equal(log.inflight, 0);
  assert.equal(log.entries.length, 1);
  assert.deepEqual(log.entries[0], {
    requestId: "r1",
    url: "https://example.test/a.js",
    method: "GET",
    type: "Script",
    startedAt: 1_000,
    durationMs: 250,
    status: 200,
  });
});

test("net log records network failures with the error text", () => {
  const log = new NetLog();
  log.requestStart("r1", "https://example.test/api", "POST", "Fetch", 1_000);
  log.fail("r1", "net::ERR_FAILED", 1_050);

  assert.equal(log.inflight, 0);
  assert.equal(log.entries[0]?.error, "net::ERR_FAILED");
  assert.equal(log.entries[0]?.status, undefined);
});

test("cancelled requests get a readable error instead of an empty one", () => {
  const log = new NetLog();
  log.requestStart("r1", "https://example.test/x", "GET", "XHR", 1_000);
  log.fail("r1", "", 1_010);
  assert.equal(log.entries[0]?.error, "failed");
});

test("websocket and event source traffic is ignored", () => {
  const log = new NetLog();
  log.requestStart("ws", "wss://example.test/socket", "GET", "WebSocket", 1_000);
  log.requestStart("es", "https://example.test/events", "GET", "EventSource", 1_000);
  log.finish("ws", 1_100);
  log.finish("es", 1_100);

  assert.equal(log.inflight, 0);
  assert.equal(log.entries.length, 0);
  assert.equal(log.requestsSeen, 0);
  assert.equal(log.isIgnoredType("WebSocket"), true);
});

test("the ring caps at MAX_NET_ENTRIES and keeps the newest entries", () => {
  const log = new NetLog();
  for (let index = 0; index < MAX_NET_ENTRIES + 30; index += 1) {
    const id = `r${index}`;
    log.requestStart(id, `https://example.test/${index}`, "GET", "XHR", index);
    log.finish(id, index + 1);
  }

  assert.equal(log.entries.length, MAX_NET_ENTRIES);
  assert.equal(log.entries[0]?.url, "https://example.test/30");
  assert.equal(
    log.entries[log.entries.length - 1]?.url,
    `https://example.test/${MAX_NET_ENTRIES + 29}`,
  );
});

test("restore prepends persisted entries and never exceeds the cap", () => {
  const log = new NetLog();
  log.requestStart("live", "https://example.test/live", "GET", "XHR", 5_000);
  log.finish("live", 5_010);

  const restored = Array.from({ length: MAX_NET_ENTRIES }, (_, index) => ({
    requestId: `old${index}`,
    url: `https://example.test/old/${index}`,
  }));
  log.restore(restored, 4_000);

  assert.equal(log.enabledAt, 4_000);
  assert.equal(log.entries.length, MAX_NET_ENTRIES);
  assert.equal(log.entries[0]?.requestId, "old1");
  assert.equal(log.entries[log.entries.length - 1]?.requestId, "live");
});

test("a navigation restarts the quiet window", () => {
  const log = new NetLog();
  log.noteNavigation(10_000);
  assert.equal(log.tracker.isQuiet(10_400, 500), false);
  assert.equal(log.tracker.isQuiet(10_500, 500), true);
});

test("inflightExcludingLongPoll ignores pending requests older than longPollMs", () => {
  assert.equal(DEFAULT_LONG_POLL_MS, 2_000);
  const log = new NetLog();
  log.requestStart("r1", "https://example.test/stream", "GET", "Fetch", 1_000);
  log.requestStart("r2", "https://example.test/recent", "GET", "Fetch", 3_500);

  assert.equal(log.inflight, 2);
  // At now = 4_000: r1 started 3_000ms ago (> 2_000ms), r2 started 500ms ago (< 2_000ms)
  assert.equal(log.inflightExcludingLongPoll(4_000, 2_000), 1);

  // At now = 6_000: both are older than 2_000ms
  assert.equal(log.inflightExcludingLongPoll(6_000, 2_000), 0);

  // Custom longPollMs = 5_000ms: at 4_000, r1 is 3s old (< 5s)
  assert.equal(log.inflightExcludingLongPoll(4_000, 5_000), 2);
});
