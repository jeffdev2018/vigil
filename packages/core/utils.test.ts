import { afterEach, describe, expect, it, vi } from "vitest";
import {
  createRequestId,
  createSafeId,
  generateUUID,
  humanizeIdentifier,
  isImeComposing,
  runBulk,
  truncateWithEllipsis,
} from "./utils";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("utils id helpers", () => {
  it("generateUUID returns a valid UUID v4", () => {
    const id = generateUUID();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  });

  it("createSafeId falls back when crypto.randomUUID is unavailable", () => {
    vi.stubGlobal("crypto", {
      getRandomValues: (arr: Uint8Array) => {
        for (let i = 0; i < arr.length; i++) arr[i] = i;
        return arr;
      },
    });

    const id = createSafeId();
    expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  });

  it("createRequestId defaults to length 8 and respects custom length", () => {
    vi.spyOn(globalThis.crypto, "randomUUID").mockReturnValue("12345678-1234-4abc-8def-1234567890ab");

    expect(createRequestId()).toBe("12345678");
    expect(createRequestId(12)).toBe("123456781234");
  });
});

describe("isImeComposing", () => {
  it("returns true when nativeEvent.isComposing is set (React synthetic event)", () => {
    expect(isImeComposing({ nativeEvent: { isComposing: true, keyCode: 13 } })).toBe(true);
  });

  it("returns true when nativeEvent.keyCode is 229 (Safari edge case)", () => {
    // Safari clears isComposing on the keydown that ends composition; keyCode
    // stays 229 throughout, which is the only reliable signal in that browser.
    expect(isImeComposing({ nativeEvent: { isComposing: false, keyCode: 229 } })).toBe(true);
  });

  it("returns true for native KeyboardEvent without nativeEvent wrapper", () => {
    expect(isImeComposing({ isComposing: true, keyCode: 13 })).toBe(true);
    expect(isImeComposing({ isComposing: false, keyCode: 229 })).toBe(true);
  });

  it("returns false when not composing", () => {
    expect(isImeComposing({ nativeEvent: { isComposing: false, keyCode: 13 } })).toBe(false);
    expect(isImeComposing({ isComposing: false, keyCode: 13 })).toBe(false);
  });
});

describe("truncateWithEllipsis", () => {
  it("returns text shorter than maxLength unchanged", () => {
    expect(truncateWithEllipsis("hello", 10)).toBe("hello");
  });

  it("returns text exactly equal to maxLength unchanged", () => {
    expect(truncateWithEllipsis("hello", 5)).toBe("hello");
  });

  it("truncates longer text and appends an ellipsis within maxLength", () => {
    const result = truncateWithEllipsis("hello world", 8);
    expect(result).toBe("hello w…");
    expect(Array.from(result)).toHaveLength(8);
    expect(result.endsWith("…")).toBe(true);
  });

  it("trims trailing whitespace before the ellipsis", () => {
    // The cut lands right after the space; it must not leave "hello …".
    expect(truncateWithEllipsis("hello world", 7)).toBe("hello…");
  });

  it("never splits an astral character into a lone surrogate", () => {
    const result = truncateWithEllipsis("🎉🎉🎉🎉🎉 party", 8);
    expect(result).toBe("🎉🎉🎉🎉🎉 p…");
    expect(Array.from(result)).toHaveLength(8);
    // No unpaired high surrogate left dangling.
    expect(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])/.test(result)).toBe(false);
  });

  it("measures the limit in code points, not UTF-16 code units", () => {
    // Five emoji are 10 code units but 5 visible characters, so they fit
    // within maxLength 8 and must not be truncated.
    expect(truncateWithEllipsis("🎉🎉🎉🎉🎉", 8)).toBe("🎉🎉🎉🎉🎉");
  });

  it("truncates CJK content by code point", () => {
    expect(truncateWithEllipsis("你好世界你好世界", 5)).toBe("你好世界…");
  });

  it("handles degenerate maxLength values without throwing", () => {
    expect(truncateWithEllipsis("hello", 1)).toBe("…");
    expect(truncateWithEllipsis("hello", 0)).toBe("");
    expect(truncateWithEllipsis("hello", -5)).toBe("");
    expect(truncateWithEllipsis("hello", Number.NaN)).toBe("…");
  });
});

describe("humanizeIdentifier", () => {
  it("turns kebab and snake identifiers into a sentence", () => {
    expect(humanizeIdentifier("handle-out-of-scope-tasks")).toBe("Handle out of scope tasks");
    expect(humanizeIdentifier("context_overflow")).toBe("Context overflow");
    expect(humanizeIdentifier("mixed-case_id")).toBe("Mixed case id");
  });

  it("leaves human-authored names untouched", () => {
    expect(humanizeIdentifier("Repo triage")).toBe("Repo triage");
    expect(humanizeIdentifier("PR Review")).toBe("PR Review");
  });

  it("returns the input when nothing readable remains", () => {
    expect(humanizeIdentifier("")).toBe("");
    expect(humanizeIdentifier("---")).toBe("---");
  });
});

describe("runBulk", () => {
  it("reports every item as succeeded when all resolve", async () => {
    const result = await runBulk([1, 2, 3], async (n) => n * 2);
    expect(result.succeeded).toEqual([1, 2, 3]);
    expect(result.failed).toEqual([]);
  });

  it("keeps going past a rejection and reports partial failure with the original error", async () => {
    const boom = new Error("boom");
    const result = await runBulk(["a", "b", "c"], async (item) => {
      if (item === "b") throw boom;
      return item;
    });
    expect(result.succeeded).toEqual(["a", "c"]);
    expect(result.failed).toEqual([{ item: "b", error: boom }]);
  });

  it("returns empty results for an empty input without calling fn", async () => {
    const fn = vi.fn();
    const result = await runBulk([], fn);
    expect(result).toEqual({ succeeded: [], failed: [] });
    expect(fn).not.toHaveBeenCalled();
  });
});
