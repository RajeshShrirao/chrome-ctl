import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const dist = path.join(root, "dist");
const REQUIRED_PERMISSIONS = [
  "activeTab",
  "debugger",
  "offscreen",
  "scripting",
  "storage",
  "tabs",
];

const failures = [];

function check(condition, message) {
  if (!condition) {
    failures.push(message);
  }
}

async function fileExists(filePath) {
  try {
    const info = await stat(filePath);
    return info.isFile();
  } catch {
    return false;
  }
}

let manifest;
try {
  manifest = JSON.parse(await readFile(path.join(dist, "manifest.json"), "utf8"));
} catch (error) {
  console.error(`verify: cannot read dist/manifest.json (${error.message})`);
  process.exit(1);
}

check(manifest.manifest_version === 3, "manifest_version must be 3");

const permissions = Array.isArray(manifest.permissions) ? [...manifest.permissions].sort() : [];
check(
  JSON.stringify(permissions) === JSON.stringify(REQUIRED_PERMISSIONS),
  `permissions must be exactly [${REQUIRED_PERMISSIONS.join(", ")}], got [${permissions.join(", ")}]`,
);

check(
  Array.isArray(manifest.host_permissions) && manifest.host_permissions.includes("<all_urls>"),
  'host_permissions must include "<all_urls>"',
);

check(
  manifest.background?.type === "module",
  'background.type must be "module"',
);

const serviceWorker = manifest.background?.service_worker;
check(
  typeof serviceWorker === "string" && (await fileExists(path.join(dist, serviceWorker))),
  `background.service_worker must reference an existing dist file (got ${String(serviceWorker)})`,
);

const popup = manifest.action?.default_popup;
check(
  typeof popup === "string" && (await fileExists(path.join(dist, popup))),
  `action.default_popup must reference an existing dist file (got ${String(popup)})`,
);

check(
  !("content_scripts" in manifest),
  "content_scripts must not be declared in Phase 1",
);

check(
  await fileExists(path.join(dist, "offscreen", "offscreen.html")),
  "dist/offscreen/offscreen.html is missing",
);
check(
  await fileExists(path.join(dist, "offscreen", "offscreen.js")),
  "dist/offscreen/offscreen.js is missing",
);

if (failures.length > 0) {
  console.error("verify: dist/ does not match the Phase 1 manifest contract:");
  for (const failure of failures) {
    console.error(`  - ${failure}`);
  }
  process.exit(1);
}

console.log(
  "verify: OK (manifest v3, exact permissions, module service worker, popup, offscreen assets, no content scripts)",
);
