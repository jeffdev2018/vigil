#!/usr/bin/env node
// Compiles the `mic-monitor` Swift helper (ambient meeting detection) into
// apps/desktop/resources/bin/ so electron-vite (dev) and electron-builder
// (prod, via `asarUnpack: resources/**`) pick it up — the same directory and
// the same dev/packaged resolution as the bundled Go CLI.
//
// MUST run after bundle-cli.mjs: that script wipes resources/bin/ before it
// copies the CLI in.
//
// Best-effort by design. macOS-only feature, and without swiftc there is
// simply no helper — the main process checks the binary exists and leaves
// ambient detection off. Never fails the build.

import { chmod, mkdir, stat } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktopRoot = resolve(here, "..");
const swiftSrc = join(desktopRoot, "native", "mic-monitor.swift");
const destDir = join(desktopRoot, "resources", "bin");
const destBinary = join(destDir, "mic-monitor");

function targetPlatformFromArgs(argv) {
  const flagIndex = argv.indexOf("--target-platform");
  if (flagIndex === -1) return process.platform;
  return argv[flagIndex + 1] ?? "";
}

const targetPlatform = targetPlatformFromArgs(process.argv.slice(2));

// Cross-compiling the helper is not supported: swiftc here only produces a
// host-native binary, so a non-darwin target (or host) simply ships without.
if (process.platform !== "darwin" || targetPlatform !== "darwin") {
  console.log(
    "[bundle-mic-monitor] not a macOS host/target — skipping (ambient meeting detection is macOS-only).",
  );
  process.exit(0);
}

async function mtimeMs(path) {
  try {
    return (await stat(path)).mtimeMs;
  } catch {
    return null;
  }
}

const builtAt = await mtimeMs(destBinary);
const sourceAt = await mtimeMs(swiftSrc);
if (sourceAt === null) {
  console.warn(`[bundle-mic-monitor] ${swiftSrc} missing — skipping.`);
  process.exit(0);
}
if (builtAt !== null && builtAt >= sourceAt) {
  console.log("[bundle-mic-monitor] helper up to date");
  process.exit(0);
}

await mkdir(destDir, { recursive: true });
try {
  execFileSync("swiftc", ["-O", swiftSrc, "-o", destBinary], { stdio: "inherit" });
} catch (err) {
  console.warn(
    "[bundle-mic-monitor] swiftc unavailable or compile failed — ambient " +
      `meeting detection stays off (${err instanceof Error ? err.message : err}).`,
  );
  process.exit(0);
}
await chmod(destBinary, 0o755);

// Ad-hoc sign so Gatekeeper does not block the parent app spawning the child,
// same as bundle-cli.mjs does for the Go binary.
try {
  execFileSync("codesign", ["-s", "-", "--force", destBinary], { stdio: "pipe" });
} catch {
  // Non-fatal: unsigned helpers still run when the parent app is trusted.
}

console.log(`[bundle-mic-monitor] compiled ${swiftSrc} → ${destBinary}`);
