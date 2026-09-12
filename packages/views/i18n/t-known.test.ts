// @vitest-environment node
import i18next from "i18next";
import { describe, expect, it } from "vitest";
import { tKnown } from "./t-known";

// `resources`/`getFixedT`'s namespace param are typed against the app-wide
// I18nResources augmentation (see resources-types.ts); this test bundle
// deliberately isn't a registered namespace, so the instance is treated as
// loosely typed here — isolated to this fixture, not the helper under test.
function makeT() {
  const instance = i18next.createInstance() as unknown as {
    init: (opts: Record<string, unknown>) => void;
    getFixedT: (lng: string, ns: string) => unknown;
  };
  instance.init({
    lng: "en",
    resources: { en: { test: { modes: { propose: "Propose", autonomous: "Autonomous" } } } },
    interpolation: { escapeValue: false },
    initAsync: false,
  });
  // Matches production: useT() returns react-i18next's getFixedT() result,
  // which has no `.exists` method — see t-known.ts for why.
  return instance.getFixedT("en", "test") as Parameters<typeof tKnown>[0];
}

describe("tKnown", () => {
  it("resolves a known key", () => {
    const t = makeT();
    expect(tKnown(t, "modes", "propose", "fallback")).toBe("Propose");
  });

  it("returns the fallback for a key unknown to the bundle (e.g. a server enum value ahead of the frontend release)", () => {
    const t = makeT();
    expect(tKnown(t, "modes", "some_future_mode", "fallback")).toBe("fallback");
  });
});
