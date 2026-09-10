import { describe, expect, it } from "vitest";
import {
  FIRST_RUN_ISSUE_BODY,
  FIRST_RUN_ISSUE_TITLE,
  pickContentLang,
} from "./index";

describe("FIRST_RUN_ISSUE content", () => {
  it("has a title and body for every content language pickContentLang can pick", () => {
    for (const lang of ["en", "zh", "ko", "ja"] as const) {
      expect(FIRST_RUN_ISSUE_TITLE[lang].length).toBeGreaterThan(0);
      expect(FIRST_RUN_ISSUE_BODY[lang].length).toBeGreaterThan(0);
    }
  });
});

describe("pickContentLang", () => {
  it("uses the shared locale matcher before selecting persisted content", () => {
    expect(pickContentLang("en-US")).toBe("en");
    expect(pickContentLang("zh-Hant")).toBe("zh");
    expect(pickContentLang("ko-KR")).toBe("ko");
    expect(pickContentLang("ja-JP")).toBe("ja");
  });

  it("falls back to English for unsupported or missing languages", () => {
    expect(pickContentLang("fr-FR")).toBe("en");
    expect(pickContentLang(null)).toBe("en");
    expect(pickContentLang(undefined)).toBe("en");
  });
});
