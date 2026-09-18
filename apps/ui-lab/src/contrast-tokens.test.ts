// @vitest-environment node
//
// Regression guard for the actual color VALUES in tokens.css, not the
// contrast calculator itself (see contrast.test.ts for that). This is the
// test that was missing: --warning was able to sit at 2.17:1 against
// --page-canvas as plain text/icon color for a long time because nothing
// measured the system's token pairs, only the math primitives. Every case
// below fails loudly, naming the token, the surface, the measured ratio and
// the required minimum, if a future token edit regresses contrast.
import { describe, expect, it } from "vitest";
import { contrastRatio } from "./contrast";
import { baseline } from "./tokens";

type Theme = "light" | "dark";

/**
 * tokens.css aliases some dark-theme tokens to a light-independent base
 * (e.g. `--success-subtle-foreground: var(--success)`) instead of repeating
 * the oklch value, so raw token lookups can return the literal string
 * "var(--x)". Resolve that chain within the same theme's token map. Guards
 * against a cycle so a bad edit fails fast instead of hanging.
 */
function resolve(
  tokens: Record<string, string>,
  key: string,
  seen: Set<string> = new Set(),
): string {
  const raw = tokens[key];
  if (raw === undefined) {
    throw new Error(`Token ${key} is not defined in this theme's scope`);
  }
  const ref = /^var\((--[\w-]+)\)$/.exec(raw)?.[1];
  if (!ref) return raw;
  if (seen.has(key)) {
    throw new Error(`Circular var() reference while resolving ${key}`);
  }
  seen.add(key);
  return resolve(tokens, ref, seen);
}

/** A CSS color value, either a `--token` (resolved against the theme) or a literal like "white". */
function colorValue(tokens: Record<string, string>, token: string): string {
  return token.startsWith("--") ? resolve(tokens, token) : token;
}

interface Pair {
  /** Token name (or CSS color literal, e.g. "white") painted as foreground. */
  foreground: string;
  /** Background layers, opaque canvas first — matches contrastRatio's own contract. */
  backgrounds: string[];
  /** WCAG minimum this pair must clear. */
  minimum: number;
}

// Neutral + tinted surfaces any of the SEMANTIC_TEXT tokens below can land
// on: list rows, menu items, nav hover states, and the new alert-banner
// tinted backgrounds from this same change.
const SURFACES = [
  "--page-canvas",
  "--surface",
  "--app-shell",
  "--muted",
  "--sidebar-accent",
  "--surface-selected",
  "--success-subtle",
  "--warning-subtle",
  "--info-subtle",
  "--destructive-subtle",
];

// Text/icon role tokens: must clear the 4.5:1 text floor on every surface
// above. --warning, --success and --destructive (the base fill tokens) are
// deliberately NOT in this list — they are tuned for solid fills, not text.
// --destructive looks text-safe when checked only against --page-canvas /
// --surface (4.60-4.76:1, the two surfaces the earlier spot-check used), but
// measured against the full surface set here it fails on --sidebar-accent
// (3.93:1) and every --*-subtle background (4.26-4.33:1) — the same failure
// shape as --warning, just smaller. --destructive-strong is the token
// that's actually safe everywhere (worst case --sidebar-accent 4.51:1).
const SEMANTIC_TEXT_TOKENS = [
  "--success-strong",
  "--warning-strong",
  "--info-strong",
  "--destructive-strong",
  "--muted-foreground",
];

const textPairs: Pair[] = SEMANTIC_TEXT_TOKENS.flatMap((foreground) =>
  SURFACES.map((background) => ({
    foreground,
    backgrounds: [background],
    minimum: 4.5,
  })),
);

// Solid-fill pairs: the role token IS the whole background (a button, a
// primary/brand surface), with a fixed foreground on top -- composited
// directly on the saturated fill, unlike the tinted "subtle" pattern above.
const filledPairs: Pair[] = [
  { foreground: "--primary-foreground", backgrounds: ["--primary"], minimum: 4.5 },
  // KNOWN RED (dark theme only): --brand-foreground on --brand measures
  // 3.12:1 in dark mode, a pre-existing pair this change did not introduce
  // and is not touching. --brand is deliberately not a solid-fill role
  // outside this pairing (tokens.css: "Brand also paints body-text links"),
  // so retuning it risks the rest of its usage. Left red on purpose per
  // review — flagged in the PR for a design call, not silenced here.
  { foreground: "--brand-foreground", backgrounds: ["--brand"], minimum: 4.5 },
  // quick-create-issue.tsx's "just sent" confirmation state paints
  // --success-foreground directly on solid --success (the only remaining
  // solid-fill + fixed-text badge/button pattern this change left in
  // place). --success-foreground exists (rather than reusing "white")
  // specifically because plain white fails here in dark mode (3.05:1).
  { foreground: "--success-foreground", backgrounds: ["--success"], minimum: 4.5 },
];

// Non-text marks only (dividers, chevrons, the em dash for an empty cell,
// empty-state glyphs) -- WCAG 1.4.11 sets a 3:1 floor for these "graphical
// objects required to understand content", not the 4.5:1 text floor.
// --faint-foreground is deliberately restricted to this list: tokens.css
// documents it as NOT valid for text (AA caps a lighter text tone 0.018 away
// from --muted-foreground here, a difference nobody can see, so there is no
// room for a third readable text step below --muted-foreground).
const nonTextPairs: Pair[] = SURFACES.slice(0, 6).map((background) => ({
  foreground: "--faint-foreground",
  backgrounds: [background],
  minimum: 3,
}));

const allPairs = [...textPairs, ...filledPairs, ...nonTextPairs];

describe.each<Theme>(["light", "dark"])("%s theme token contrast", (theme) => {
  const tokens = baseline[theme];

  it.each(allPairs.map((pair) => ({ ...pair, theme })))(
    "$foreground on $backgrounds >= $minimum:1 ($theme)",
    ({ foreground, backgrounds, minimum }) => {
      const fg = colorValue(tokens, foreground);
      const bg = backgrounds.map((b) => colorValue(tokens, b));
      const ratio = contrastRatio(fg, bg);
      expect(
        ratio,
        `${foreground} on ${backgrounds.join(" > ")} (${theme} theme) measured ${ratio.toFixed(2)}:1, needs >= ${minimum}:1`,
      ).toBeGreaterThanOrEqual(minimum);
    },
  );
});
