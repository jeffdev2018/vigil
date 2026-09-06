import { describe, expect, it } from "vitest";
import { describeVersion } from "./cli-version.mjs";

describe("describeVersion", () => {
  it("keeps tagged and git-describe shapes untouched", () => {
    expect(describeVersion("v0.4.30", "12", "abc1234")).toBe("v0.4.30");
    expect(describeVersion("v0.2.15-235-gdaf0e935", "9", "x")).toBe("v0.2.15-235-gdaf0e935");
    expect(describeVersion("v0.2.15-235-gdaf0e935-dirty", "9", "x")).toBe(
      "v0.2.15-235-gdaf0e935-dirty",
    );
  });

  it("synthesizes the describe shape when no v* tag is reachable", () => {
    // A bare hash is what `git describe --always` yields on a fork; the server
    // CLI gate treats it as "no version" and refuses agent quick-create.
    expect(describeVersion("14c497f89", "5604", "14c497f89")).toBe("v0.0.0-5604-g14c497f89");
    expect(describeVersion("14c497f89-dirty", "5604", "14c497f89")).toBe(
      "v0.0.0-5604-g14c497f89-dirty",
    );
    expect(describeVersion("", "", "abc")).toBe("v0.0.0-0-gabc");
  });
});
