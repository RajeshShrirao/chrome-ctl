import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { ActivityFeed, computeBadge, summarizeRpc } from "../src/background/ui_state.ts";

describe("computeBadge", () => {
  it("prioritizes disconnected over paused, running, and error", () => {
    assert.deepEqual(
      computeBadge({ connState: "disconnected", paused: true, inflightCount: 2, hasError: true }),
      { text: "!", color: "#f85149" },
    );
    assert.deepEqual(
      computeBadge({ connState: "unconfigured", paused: false, inflightCount: 0, hasError: false }),
      { text: "!", color: "#f85149" },
    );
  });

  it("prioritizes paused over running and error when connected", () => {
    assert.deepEqual(
      computeBadge({ connState: "connected", paused: true, inflightCount: 1, hasError: true }),
      { text: "||", color: "#d29922" },
    );
  });

  it("shows RUN while inflight and ERR after a failure", () => {
    assert.deepEqual(
      computeBadge({ connState: "connected", paused: false, inflightCount: 1, hasError: true }),
      { text: "RUN", color: "#4c8dff" },
    );
    assert.deepEqual(
      computeBadge({ connState: "connected", paused: false, inflightCount: 0, hasError: true }),
      { text: "ERR", color: "#f85149" },
    );
  });

  it("clears badge when connected and idle with no error", () => {
    assert.deepEqual(
      computeBadge({ connState: "connected", paused: false, inflightCount: 0, hasError: false }),
      { text: "", color: "#3fb950" },
    );
  });
});

describe("ActivityFeed", () => {
  it("caps entries at maxEntries and updates status on finish", () => {
    const feed = new ActivityFeed(3);
    feed.start({ id: "1", method: "page.snap", summary: "viewport", startedAt: 100 });
    feed.start({ id: "2", method: "action.click", summary: "[4]", startedAt: 200 });
    feed.start({ id: "3", method: "action.type", summary: "[5] 3 chars", startedAt: 300 });
    feed.start({ id: "4", method: "page.wait", summary: "network", startedAt: 400 });

    const items = feed.list();
    assert.equal(items.length, 3);
    assert.deepEqual(items.map((e) => e.id), ["2", "3", "4"]);

    feed.finish("4", false, 123, "timeout");
    const last = feed.list()[2];
    assert.equal(last.status, "error");
    assert.equal(last.durationMs, 123);
    assert.equal(last.error, "timeout");
  });
});

describe("summarizeRpc", () => {
  it("formats common methods concisely", () => {
    assert.equal(summarizeRpc("tab.open", { url: "https://news.ycombinator.com/newest" }), "news.ycombinator.com/newest");
    assert.equal(summarizeRpc("action.click", { id: 12, doubleClick: true }), "[12] (2x)");
    assert.equal(summarizeRpc("action.type", { id: 3, text: "hello", enter: true }), "[3] 5 chars +Enter");
    assert.equal(summarizeRpc("page.wait", { network: true, text: "Done" }), 'network + "Done"');
    assert.equal(summarizeRpc("page.snap", { diff: true, all: true }), "--diff --all");
  });
});
