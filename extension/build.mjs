import { watch as watchFs } from "node:fs";
import { copyFile, mkdir, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build, context } from "esbuild";

const root = path.dirname(fileURLToPath(import.meta.url));
const src = path.join(root, "src");
const dist = path.join(root, "dist");
const watchMode = process.argv.includes("--watch");

/** @type {import("esbuild").BuildOptions} */
const options = {
  entryPoints: [
    path.join(src, "background", "service_worker.ts"),
    path.join(src, "offscreen", "offscreen.ts"),
    path.join(src, "popup", "popup.ts"),
  ],
  outbase: src,
  outdir: dist,
  bundle: true,
  format: "esm",
  target: "chrome116",
  sourcemap: true,
  platform: "browser",
  logLevel: "info",
};

const staticFiles = [
  [path.join(root, "manifest.json"), path.join(dist, "manifest.json")],
  [path.join(src, "offscreen", "offscreen.html"), path.join(dist, "offscreen", "offscreen.html")],
  [path.join(src, "popup", "popup.html"), path.join(dist, "popup", "popup.html")],
  [path.join(src, "popup", "popup.css"), path.join(dist, "popup", "popup.css")],
];

async function copyStatic() {
  await mkdir(path.join(dist, "offscreen"), { recursive: true });
  await mkdir(path.join(dist, "popup"), { recursive: true });
  await Promise.all(staticFiles.map(([from, to]) => copyFile(from, to)));
}

async function runOnce() {
  await rm(dist, { recursive: true, force: true });
  await build(options);
  await copyStatic();
  console.log("static assets copied to dist/");
}

async function runWatch() {
  await rm(dist, { recursive: true, force: true });
  await copyStatic();

  const ctx = await context(options);
  await ctx.watch();

  for (const [from] of staticFiles) {
    watchFs(from, () => {
      copyStatic().catch((error) => console.error("static copy failed:", error));
    });
  }
  console.log("watching src/ and static assets (Ctrl+C to stop)");
}

try {
  if (watchMode) {
    await runWatch();
  } else {
    await runOnce();
  }
} catch (error) {
  console.error(error);
  process.exit(1);
}
