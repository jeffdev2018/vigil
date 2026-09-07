/**
 * Provider-free proof that the fixture recipe works:
 * 1) reproduction fails
 * 2) fixed path passes
 * Exits non-zero if either stage is wrong.
 */
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";

const root = path.dirname(fileURLToPath(import.meta.url));

function run(env = {}) {
  return spawnSync(
    process.execPath,
    ["--test", "broken.test.js"],
    { cwd: root, env: { ...process.env, ...env }, encoding: "utf8" },
  );
}

const broken = run();
if (broken.status === 0) {
  console.error("expected reproduction to fail, but tests passed");
  process.exit(1);
}
console.log("reproduce: failing test observed");

const fixed = run({ FIX_APPLIED: "1" });
if (fixed.status !== 0) {
  console.error("expected fixed path to pass");
  console.error(fixed.stdout);
  console.error(fixed.stderr);
  process.exit(1);
}
console.log("fix+test: FIX_APPLIED=1 passes");
console.log("open_pr + delivery_review: use Multica issue + Delivery review (no provider call in this proof)");
console.log("OK");
