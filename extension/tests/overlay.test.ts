import assert from "node:assert/strict";
import { describe, it } from "node:test";
import { JSDOM } from "jsdom";
import { setAutomationOverlay } from "../src/content/overlay.ts";

describe("setAutomationOverlay", () => {
  it("mounts a closed shadow host on idle/running/paused and removes it on off", () => {
    const dom = new JSDOM("<!doctype html><html><body><p>Hello</p></body></html>");
    const doc = dom.window.document;

    setAutomationOverlay("idle", doc);
    const host = doc.documentElement.querySelector("[data-chrome-ctl-overlay]");
    assert.ok(host, "overlay host should be mounted");
    assert.equal(host.shadowRoot, null, "shadow root must be closed");

    setAutomationOverlay("running", doc);
    assert.equal(
      doc.documentElement.querySelectorAll("[data-chrome-ctl-overlay]").length,
      1,
      "updating state should reuse the single host",
    );

    setAutomationOverlay("paused", doc);
    assert.equal(
      doc.documentElement.querySelectorAll("[data-chrome-ctl-overlay]").length,
      1,
    );

    setAutomationOverlay("off", doc);
    assert.equal(
      doc.documentElement.querySelector("[data-chrome-ctl-overlay]"),
      null,
      "overlay host should be removed when off",
    );
  });
});
