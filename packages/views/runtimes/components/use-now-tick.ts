import { useEffect, useState } from "react";

/**
 * Re-renders every `intervalMs` so relative times ("3 min ago") stay
 * current. Its own module rather than `shared.tsx`: the runtime tests mock
 * `./shared` export by export, and a hook there would have to be re-mocked.
 */
export function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}
