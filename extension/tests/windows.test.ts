import test from "node:test";
import assert from "node:assert/strict";

import {
  AUTOMATION_WINDOW_KEY,
  automationActiveTab,
  liveAutomationWindowId,
  readAutomationWindowId,
  writeAutomationWindowId,
} from "../src/background/windows.ts";

interface FakeChrome {
  storage: { local: { get: (k: string) => Promise<Record<string, unknown>>; remove: (k: string) => Promise<void>; set: (v: Record<string, unknown>) => Promise<void> } };
  windows: { get: (id: number) => Promise<unknown> };
  tabs: { query: (q: Record<string, unknown>) => Promise<unknown[]> };
}

function installFake(options: { stored?: unknown; liveWindow?: boolean } = {}): {
  chrome: FakeChrome;
  stored: Map<string, unknown>;
} {
  const stored = new Map<string, unknown>();
  if (options.stored !== undefined) {
    stored.set(AUTOMATION_WINDOW_KEY, options.stored);
  }
  const fake: FakeChrome = {
    storage: {
      local: {
        get: async (key) => ({ ...(stored.has(key) ? { [key]: stored.get(key) } : {}) }),
        remove: async (key) => void stored.delete(key),
        set: async (value) => {
          for (const [key, item] of Object.entries(value)) stored.set(key, item);
        },
      },
    },
    windows: {
      get: async (id) => {
        if (options.liveWindow === false) throw new Error("No window with id: " + id);
        return { id };
      },
    },
    tabs: {
      query: async () => [{ id: 42, windowId: 7, active: true }],
    },
  };
  (globalThis as unknown as { chrome: FakeChrome }).chrome = fake;
  return { chrome: fake, stored };
}

test("readAutomationWindowId only accepts an integer", async () => {
  installFake({ stored: 7 });
  assert.equal(await readAutomationWindowId(), 7);

  installFake({ stored: "7" });
  assert.equal(await readAutomationWindowId(), undefined);

  installFake({});
  assert.equal(await readAutomationWindowId(), undefined);
});

test("writeAutomationWindowId stores and clears the key", async () => {
  const { stored } = installFake();
  await writeAutomationWindowId(12);
  assert.equal(stored.get(AUTOMATION_WINDOW_KEY), 12);

  await writeAutomationWindowId(undefined);
  assert.equal(stored.has(AUTOMATION_WINDOW_KEY), false);
});

test("liveAutomationWindowId drops a window the user closed", async () => {
  const { stored } = installFake({ stored: 99, liveWindow: false });
  assert.equal(await liveAutomationWindowId(), undefined);
  assert.equal(stored.has(AUTOMATION_WINDOW_KEY), false);
});

test("liveAutomationWindowId keeps a window that is still open", async () => {
  installFake({ stored: 99 });
  assert.equal(await liveAutomationWindowId(), 99);
});

test("automationActiveTab resolves inside the tracked window", async () => {
  installFake({ stored: 7 });
  const tab = await automationActiveTab();
  assert.equal(tab?.id, 42);

  installFake({});
  assert.equal(await automationActiveTab(), undefined);
});
