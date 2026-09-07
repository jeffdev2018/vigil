/**
 * Deliberately wrong adder used by the Multica bug-fix recipe fixture.
 * The guided path proves reproduce → fix → test without a provider agent:
 * `npm test` fails; apply the fix below (or set FIX_APPLIED=1) and
 * `npm run test:fixed` passes.
 */
export function add(a, b) {
  if (process.env.FIX_APPLIED === "1") {
    return a + b;
  }
  // Bug: subtracts instead of adding.
  return a - b;
}
