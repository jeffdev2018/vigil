import { beforeAll } from "vitest";

// Node 25 ships a partial `localStorage` under jsdom that's missing
// `clear` / `removeItem` / sometimes `setItem`. Replace it with an in-memory
// Storage so persist helpers and cleanup paths can round-trip.
beforeAll(() => {
  const current = globalThis.localStorage;
  if (
    typeof current?.clear === "function" &&
    typeof current?.setItem === "function" &&
    typeof current?.removeItem === "function" &&
    typeof current?.getItem === "function"
  ) {
    return;
  }
  const values = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (k) => values.get(k) ?? null,
    key: (i) => Array.from(values.keys())[i] ?? null,
    removeItem: (k) => {
      values.delete(k);
    },
    setItem: (k, v) => {
      values.set(k, v);
    },
  };
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: storage,
    });
  }
});
