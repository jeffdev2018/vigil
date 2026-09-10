import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { CheckCircle2, Search } from "lucide-react";
import type { TimelineEntry } from "@multica/core/types";
import { knownA2AIntent } from "@multica/core/issues/a2a-message";
import { useActorName } from "@multica/core/workspace/hooks";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useT } from "../../i18n";
import { pickerNavigationDirection } from "../../common/picker-keys";
import { isImeComposing } from "@multica/core/utils";
import { matchesThreadFilter, type ThreadOutlineFilter } from "./thread-utils";

// ---------------------------------------------------------------------------
// ThreadMinimap — quick-jump rail with a complete thread outline.
// The rail shows viewport position; hovering or focusing any tick opens one
// stationary, scrollable list of every thread title. Rows jump to the same
// timeline anchors as the ticks, including folded resolved threads.

/** Minimum number of threads before the rail is worth its pixels. */
const MIN_THREADS = 2;

/** Intent delay before the card first appears; gliding afterwards is instant. */
const PREVIEW_OPEN_DELAY_MS = 150;
/** Grace period on leave — long enough to travel from rail onto the card. */
const PREVIEW_CLOSE_DELAY_MS = 150;

// ---------------------------------------------------------------------------
// Hover wave — Dock-style proximity magnification
// ---------------------------------------------------------------------------
//
// While the pointer travels along the rail, every tick scales with a cosine
// falloff of its distance to the cursor, so the hovered tick peaks and its
// neighbours taper off like a wave. Driven per-pointermove with direct style
// writes (no React re-render), batched read-then-write inside one rAF, on the
// compositor-friendly native `scale` property; the 100ms ease-out transition
// on the tick smooths between pointer samples and settles the collapse on
// leave. Only the hovered tick darkens — neighbours grow but keep their color.

/** Distance (px) at which a tick stops feeling the wave — ~4 tick pitches. */
const WAVE_RADIUS_PX = 56;
/** Peak horizontal scale of the hovered tick (12px base → ~20px). */
const WAVE_MAX_SCALE = 1.7;

/**
 * Horizontal scale for a tick whose center is `distancePx` from the pointer.
 * Cosine-squared bell: smooth at the peak and at the radius edge (no kinks).
 */
export function waveScale(distancePx: number): number {
  const d = Math.abs(distancePx);
  if (d >= WAVE_RADIUS_PX) return 1;
  const t = Math.cos(((d / WAVE_RADIUS_PX) * Math.PI) / 2);
  return 1 + (WAVE_MAX_SCALE - 1) * t * t;
}

/**
 * Caps applied by `commentPreview`. Outline labels truncate visually,
 * but agent comments can be tens of KB of
 * markdown — capping here keeps the flattened strings (and the aria-labels
 * derived from them) small instead of shipping the whole comment into the DOM.
 */
const PREVIEW_TITLE_MAX = 200;
const PREVIEW_BODY_MAX = 300;

/**
 * Flatten comment markdown into a plain-text preview: `title` is the first
 * non-empty line (bold in the card), `body` is the remaining lines joined
 * into one muted excerpt. Mirrors the chat list's `toPreview` flattening
 * (fences dropped, md tokens stripped) but keeps the first-line/body split
 * the minimap card renders.
 */
export function commentPreview(markdown: string): { title: string; body: string } {
  const lines = markdown
    .replace(/```[\s\S]*?```/g, " ")
    .split(/\r?\n/)
    .map((line) =>
      line
        .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
        .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
        .replace(/^\s*(?:[-+*]|\d+[.)])\s+/, "")
        .replace(/[#*`>~]/g, "")
        .replace(/\s+/g, " ")
        .trim(),
    )
    .filter(Boolean);
  return {
    title: (lines[0] ?? "").slice(0, PREVIEW_TITLE_MAX),
    body: lines.slice(1).join(" ").slice(0, PREVIEW_BODY_MAX),
  };
}

/**
 * Split `text` on every case-insensitive occurrence of `query`, tinting the
 * matches. Without this a filtered outline asks the reader to re-find the term
 * they just typed — a row that matched on its author looks identical to one
 * that matched on its title. Uses the same `--find-match` tint as the in-page
 * find bar, so "this is your search term" reads the same everywhere.
 */
export function highlightMatches(text: string, query: string): ReactNode {
  const needle = query.trim();
  if (needle === "") return text;
  const lowerText = text.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const out: ReactNode[] = [];
  let from = 0;
  let at = lowerText.indexOf(lowerNeedle);
  while (at !== -1) {
    if (at > from) out.push(text.slice(from, at));
    out.push(
      <mark
        key={at}
        className="rounded-[3px] bg-[var(--find-match)] px-px text-[var(--find-match-foreground)]"
      >
        {text.slice(at, at + needle.length)}
      </mark>,
    );
    from = at + needle.length;
    at = lowerText.indexOf(lowerNeedle, from);
  }
  if (from < text.length) out.push(text.slice(from));
  return out;
}

export interface ThreadMinimapThread {
  /** Root comment id — also the `comment-${id}` DOM anchor of the rendered row. */
  id: string;
  /** The thread's root comment entry (preview text + author fallback). */
  entry: TimelineEntry;
  /**
   * Whether the thread carries a resolution — derived by the caller with
   * `deriveThreadResolution`, so it covers both "Resolve thread" (root) and
   * "Resolve thread with comment" (reply), and stays true while the user has
   * a folded resolved thread expanded.
   */
  resolved: boolean;
  /** Unique authors across the root and every nested reply, in first-seen order. */
  participants: TimelineEntry[];
  /**
   * The reader started this thread, answered in it, or was @mentioned in it —
   * derived by the caller with `threadInvolvesUser`. Drives the "@me" pill.
   */
  involvesMe: boolean;
}

interface ThreadMinimapProps {
  threads: ThreadMinimapThread[];
  /** The issue detail scroll container; null until its callback ref populates. */
  scrollContainerEl: HTMLElement | null;
  onJump: (threadId: string) => void;
  /** Positioning within the page (e.g. `absolute right-3 top-12 bottom-0`) — owned by the caller, like FindBar. */
  className?: string;
}

// ---------------------------------------------------------------------------
// useVisibleThreadIds — "which comment threads are on screen right now"
// ---------------------------------------------------------------------------
//
// Which threads intersect the scroll viewport, so the rail can darken their
// ticks. Deliberately the rail's alone: "on screen" is a set, not a point, and
// only a column of ticks can show a span without suggesting multiple selection.
//
// Computed from DOM rects on scroll/resize instead of an IntersectionObserver
// because Virtuoso mounts/unmounts rows while scrolling — an observer would
// lose its targets. Unmounted rows are by definition outside the (overscanned)
// viewport, so "no element" correctly counts as not visible.

function sameIdSet(a: Set<string>, b: Set<string>): boolean {
  if (a.size !== b.size) return false;
  for (const v of a) if (!b.has(v)) return false;
  return true;
}

function useVisibleThreadIds(
  threadIds: readonly string[],
  scrollContainerEl: HTMLElement | null,
): Set<string> {
  const [visibleIds, setVisibleIds] = useState<Set<string>>(() => new Set());

  useEffect(() => {
    const container = scrollContainerEl;
    if (!container) return;

    let raf = 0;
    const compute = () => {
      raf = 0;
      const rect = container.getBoundingClientRect();
      const next = new Set<string>();
      for (const id of threadIds) {
        const el = document.getElementById(`comment-${id}`);
        if (!el) continue;
        const r = el.getBoundingClientRect();
        if (r.bottom > rect.top && r.top < rect.bottom) next.add(id);
      }
      setVisibleIds((prev) => (sameIdSet(prev, next) ? prev : next));
    };
    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(compute);
    };

    compute();
    container.addEventListener("scroll", schedule, { passive: true });
    // Content height changes without scroll events: Virtuoso mounting rows
    // after first paint, streamed agent replies growing, window resizes.
    const ro = new ResizeObserver(schedule);
    ro.observe(container);
    if (container.firstElementChild) ro.observe(container.firstElementChild);
    return () => {
      container.removeEventListener("scroll", schedule);
      ro.disconnect();
      if (raf) cancelAnimationFrame(raf);
    };
  }, [threadIds, scrollContainerEl]);

  return visibleIds;
}

/** The thread currently highlighted in the outline and rail. */
/**
 * The outline row the reader is on. Keyed by thread id rather than by index:
 * the rail always draws every tick (position is not filterable) while the card
 * draws a filtered subset, so an index means two different rows in the two
 * lists as soon as a search is typed.
 */
interface PreviewAnchor {
  threadId: string;
}

function MinimapTick({
  threadId,
  label,
  inViewport,
  isHighlighted,
  isAgentMessage,
  onClick,
}: {
  threadId: string;
  label: string;
  inViewport: boolean;
  /** The corresponding outline row is active. */
  isHighlighted: boolean;
  /**
   * The thread opens on an agent-to-agent message (F19). Tinted rather than
   * badged: the rail's whole job is to answer "where am I" at a glance, and a
   * second glyph at 12x2px would be noise. The tint is a hint, never the only
   * carrier — the outline row and the card both name the intent in words.
   */
  isAgentMessage: boolean;
  onClick: React.MouseEventHandler<HTMLButtonElement>;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      data-thread-id={threadId}
      onClick={onClick}
      // 20px wide, tick flushed to the right end: with the rail inset 12px
      // (see the caller's className) the strip spans 12–32px from the panel
      // edge, which clears a classic scrollbar's ~11px gutter on one side and
      // stops exactly at the content column's 32px padding on the other — so
      // it never sits on the scrollbar nor on body text, in either scrollbar
      // mode.
      className="group/tick flex min-h-[5px] w-5 flex-[0_1_0.875rem] cursor-pointer items-center justify-end focus-visible:outline-none"
    >
      <span
        className={cn(
          // Enlargement is a right-anchored `scale` (compositor-friendly, and
          // what the JS wave writes inline), so ticks grow inward, away from
          // the scrollbar. The 100ms ease-out doubles as smoothing between
          // pointer samples and as the settle on leave.
          "h-0.5 w-3 origin-right rounded-full transition-[scale,background-color] duration-100 ease-out",
          inViewport ? "bg-foreground/70" : "bg-muted-foreground/30",
          // Agent-to-agent threads keep the viewport/out-of-viewport contrast —
          // the tint replaces the color at each level rather than flattening the
          // two into one, so the rail still reads as a position indicator first.
          isAgentMessage && (inViewport ? "bg-brand/70" : "bg-brand/30"),
          !isHighlighted && "group-hover/tick:bg-foreground",
          // CSS floor states for when no inline wave value is present:
          // the open card's tick stays grown while the pointer rests on the
          // card, keyboard focus grows without a pointer, and reduced-motion
          // swaps the wave for a plain hover grow.
          isHighlighted && "scale-x-[1.7] bg-brand",
          "group-focus-visible/tick:scale-x-[1.7]",
          !isHighlighted && "group-focus-visible/tick:bg-foreground",
          "motion-reduce:group-hover/tick:scale-x-[1.7]",
        )}
      />
    </button>
  );
}

export function ThreadMinimap({
  threads,
  scrollContainerEl,
  onJump,
  className,
}: ThreadMinimapProps) {
  const { t } = useT("issues");
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();
  const threadIds = useMemo(() => threads.map((th) => th.id), [threads]);
  const visibleIds = useVisibleThreadIds(threadIds, scrollContainerEl);

  // Flattened previews, cached per thread by content so an unrelated timeline
  // update (reaction, new reply elsewhere) doesn't re-flatten every comment.
  const prevPreviewsRef = useRef<Map<string, { content: string | undefined; preview: { title: string; body: string } }>>(new Map());
  const previews = useMemo(() => {
    const next = new Map<string, { content: string | undefined; preview: { title: string; body: string } }>();
    const byId = new Map<string, { title: string; body: string; haystack: string }>();
    for (const th of threads) {
      const cached = prevPreviewsRef.current.get(th.id);
      const preview =
        cached && cached.content === th.entry.content
          ? cached.preview
          : commentPreview(th.entry.content ?? "");
      next.set(th.id, { content: th.entry.content, preview });
      const authorName =
        th.entry.actor_name || getActorName(th.entry.actor_type, th.entry.actor_id);
      byId.set(th.id, {
        title: preview.title || authorName,
        body: preview.body,
        // Author included so "everything I can see on the row" is searchable:
        // the name is the fallback title, and often the only thing a reader
        // remembers about a thread they are looking for.
        haystack: `${preview.title}\n${preview.body}\n${authorName}`.toLowerCase(),
      });
    }
    prevPreviewsRef.current = next;
    return byId;
  }, [threads, getActorName]);

  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<ThreadOutlineFilter>("all");

  /** Threads the card lists. The rail's ticks stay unfiltered — see PreviewAnchor. */
  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return threads.filter(
      (th) =>
        matchesThreadFilter(th, filter) &&
        (needle === "" || (previews.get(th.id)?.haystack ?? "").includes(needle)),
    );
  }, [threads, filter, query, previews]);

  const counts = useMemo(
    () => ({
      all: threads.length,
      unresolved: threads.filter((th) => !th.resolved).length,
      resolved: threads.filter((th) => th.resolved).length,
      mine: threads.filter((th) => th.involvesMe).length,
    }),
    [threads],
  );

  const shimRef = useRef<HTMLDivElement | null>(null);
  const navRef = useRef<HTMLElement | null>(null);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const listRef = useRef<HTMLUListElement | null>(null);
  const searchRef = useRef<HTMLInputElement | null>(null);
  const listId = useId();
  const optionId = useCallback(
    (threadId: string) => `${listId}-${threadId}`,
    [listId],
  );

  // Hover wave + preview targeting. Pointer position lives in refs and ticks
  // are scaled with direct style writes so pointermove never re-renders the
  // component; the rAF guard coalesces bursts to one batched read-then-write
  // per frame. The same rect pass selects the corresponding outline row.
  const waveRafRef = useRef(0);
  const pointerYRef = useRef<number | null>(null);
  const reducedMotionRef = useRef(false);

  const [preview, setPreview] = useState<PreviewAnchor | null>(null);
  const previewRef = useRef<PreviewAnchor | null>(null);
  const pendingAnchorRef = useRef<PreviewAnchor | null>(null);
  const openTimerRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const showPreview = useCallback((anchor: PreviewAnchor | null) => {
    previewRef.current = anchor;
    setPreview((prev) => (prev?.threadId === anchor?.threadId ? prev : anchor));
  }, []);

  useEffect(() => {
    reducedMotionRef.current = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    return () => {
      if (waveRafRef.current) cancelAnimationFrame(waveRafRef.current);
      if (openTimerRef.current !== null) window.clearTimeout(openTimerRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, []);

  const cancelClose = useCallback(() => {
    if (closeTimerRef.current !== null) {
      window.clearTimeout(closeTimerRef.current);
      closeTimerRef.current = null;
    }
  }, []);
  const scheduleClose = useCallback(() => {
    cancelClose();
    if (openTimerRef.current !== null) {
      window.clearTimeout(openTimerRef.current);
      openTimerRef.current = null;
    }
    closeTimerRef.current = window.setTimeout(() => {
      closeTimerRef.current = null;
      if (!shimRef.current?.contains(document.activeElement)) showPreview(null);
    }, PREVIEW_CLOSE_DELAY_MS);
  }, [cancelClose, showPreview]);

  const handleJump = useCallback((threadId: string, event: React.MouseEvent<HTMLButtonElement>) => {
    // Mouse clicks must not pin a hover outline through leftover button focus.
    // Keyboard activation keeps focus so the reader can continue navigating.
    if (event.detail > 0) {
      event.currentTarget.blur();
      // Blur schedules a close; keep the card until the pointer actually leaves.
      cancelClose();
    }
    onJump(threadId);
  }, [cancelClose, onJump]);

  const runWave = useCallback(() => {
    waveRafRef.current = 0;
    const nav = navRef.current;
    const shim = shimRef.current;
    if (!nav || !shim) return;
    const y = pointerYRef.current;
    const buttons = nav.querySelectorAll<HTMLButtonElement>("button");
    // Read pass, then write pass — never interleaved, one reflow at most.
    const scales: string[] = [];
    let nearest: { index: number; dist: number } | null = null;
    buttons.forEach((b, i) => {
      if (y === null) {
        scales.push("");
        return;
      }
      const r = b.getBoundingClientRect();
      const centerY = r.top + r.height / 2;
      const dist = Math.abs(y - centerY);
      const s = reducedMotionRef.current ? 1 : waveScale(y - centerY);
      scales.push(s > 1.001 ? `${s.toFixed(3)} 1` : "");
      if (!nearest || dist < nearest.dist) nearest = { index: i, dist };
    });
    buttons.forEach((b, i) => {
      const tick = b.firstElementChild as HTMLElement | null;
      if (!tick) return;
      const s = scales[i]!;
      // Clearing the inline value hands control back to the CSS floor states
      // (open-card tick / focus-visible / reduced-motion hover).
      if (s) tick.style.setProperty("scale", s);
      else tick.style.removeProperty("scale");
    });

    if (y === null || !nearest) return;
    const { index } = nearest as { index: number };
    const nearestThread = threads[index];
    if (!nearestThread) return;
    const anchor: PreviewAnchor = { threadId: nearestThread.id };
    pendingAnchorRef.current = anchor;
    if (previewRef.current) {
      // Already open: gliding highlights the matching row without moving the card.
      showPreview(anchor);
    } else if (openTimerRef.current === null) {
      openTimerRef.current = window.setTimeout(() => {
        openTimerRef.current = null;
        if (pointerYRef.current !== null) showPreview(pendingAnchorRef.current);
      }, PREVIEW_OPEN_DELAY_MS);
    }
  }, [showPreview, threads]);
  const scheduleWave = useCallback(() => {
    if (!waveRafRef.current) waveRafRef.current = requestAnimationFrame(runWave);
  }, [runWave]);
  const handleWaveMove = useCallback(
    (e: React.PointerEvent) => {
      cancelClose();
      pointerYRef.current = e.clientY;
      scheduleWave();
    },
    [cancelClose, scheduleWave],
  );
  const handleWaveLeave = useCallback(() => {
    pointerYRef.current = null;
    scheduleWave();
    scheduleClose();
  }, [scheduleWave, scheduleClose]);

  // Keyboard parity: focusing a tick opens its outline row immediately —
  // there is no pointer, so there is no accidental-hover to debounce.
  const handleFocus = useCallback(
    (e: React.FocusEvent) => {
      const nav = navRef.current;
      const shim = shimRef.current;
      const btn = (e.target as HTMLElement).closest("button");
      if (!nav || !shim || !btn) return;
      cancelClose();
      const threadId = (btn as HTMLButtonElement).dataset.threadId;
      if (!threadId) return;
      showPreview({ threadId });
    },
    [cancelClose, showPreview],
  );

  // Arrow keys drive the outline from the search field. Rows stay real,
  // Tab-reachable buttons whose `onFocus` sets the same anchor, so DOM focus
  // and the highlight are one piece of state rather than two that can disagree.
  const handleCardKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      // An IME commit arrives as Enter while composing, and the Ctrl aliases
      // arrive as plain letters. Acting on those would jump away mid-word the
      // moment a CJK reader picked a candidate.
      if (isImeComposing(e)) return;
      // Only from the search field: `preventDefault` on a button's keydown
      // cancels the click the browser was about to synthesize, so handling
      // Enter for the whole card made the "Resolved" pill jump instead of
      // filter, and made a focused row activate the wrong thread.
      if (e.target !== searchRef.current) return;
      const direction = pickerNavigationDirection(e.nativeEvent);
      if (direction) {
        // Matters for the letter aliases: focus is in a text field, where
        // Ctrl+K/N/P are readline editing commands that would otherwise mangle
        // the query while moving the cursor.
        e.preventDefault();
        if (rows.length === 0) return;
        const at = rows.findIndex((row) => row.id === previewRef.current?.threadId);
        const step = direction === "next" ? 1 : -1;
        const next = at < 0 ? (direction === "next" ? 0 : rows.length - 1) : at + step;
        showPreview({
          threadId: rows[(next + rows.length) % rows.length]!.id,
        });
        return;
      }
      if (e.key === "Enter") {
        const threadId = previewRef.current?.threadId;
        if (!threadId || !rows.some((row) => row.id === threadId)) return;
        e.preventDefault();
        onJump(threadId);
      }
    },
    [onJump, rows, showPreview],
  );

  const filterPills: { id: ThreadOutlineFilter; label: string; count: number }[] = [
    { id: "all", label: t(($) => $.detail.thread_outline.filter_all), count: counts.all },
    { id: "unresolved", label: t(($) => $.detail.thread_outline.filter_unresolved), count: counts.unresolved },
    { id: "resolved", label: t(($) => $.detail.thread_outline.filter_resolved), count: counts.resolved },
    { id: "mine", label: t(($) => $.detail.thread_outline.filter_mine), count: counts.mine },
  ];

  // Filtering can hide the row the anchor points at, which would leave ↵ doing
  // nothing and no row highlighted. Fall to the first remaining row instead of
  // clearing the anchor, which would close the card mid-search.
  useEffect(() => {
    const current = previewRef.current;
    if (!current || rows.length === 0) return;
    if (rows.some((row) => row.id === current.threadId)) return;
    showPreview({ threadId: rows[0]!.id });
  }, [rows, showPreview]);

  useEffect(() => {
    const card = listRef.current;
    if (!card || !preview) return;
    // Rail navigation should reveal its row in a long outline. Moving within
    // the list itself must leave its scroll position under the reader's control.
    if (
      pointerYRef.current === null &&
      !navRef.current?.contains(document.activeElement) &&
      document.activeElement !== searchRef.current
    ) {
      return;
    }
    const row = card.querySelector<HTMLLIElement>(
      `li[data-thread-id="${CSS.escape(preview.threadId)}"]`,
    );
    if (!row) return;
    if (row.offsetTop < card.scrollTop) card.scrollTop = row.offsetTop;
    else if (row.offsetTop + row.offsetHeight > card.scrollTop + card.clientHeight) {
      card.scrollTop = row.offsetTop + row.offsetHeight - card.clientHeight;
    }
  }, [preview]);

  if (threads.length < MIN_THREADS) return null;

  return (
    // Positioning shim; only the nav and the card take pointer events so the
    // strip never blocks content clicks.
    <div
      ref={shimRef}
      onKeyDown={(event) => {
        if (event.key !== "Escape") return;
        event.preventDefault();
        event.stopPropagation();
        const activeThreadId = previewRef.current?.threadId;
        if (cardRef.current?.contains(document.activeElement) && activeThreadId) {
          navRef.current
            ?.querySelector<HTMLButtonElement>(
              `button[data-thread-id="${CSS.escape(activeThreadId)}"]`,
            )
            ?.focus();
        }
        cancelClose();
        if (openTimerRef.current !== null) {
          window.clearTimeout(openTimerRef.current);
          openTimerRef.current = null;
        }
        showPreview(null);
      }}
      className={cn("pointer-events-none z-10 flex flex-col justify-center py-6", className)}
    >
      <nav
        ref={navRef}
        aria-label={t(($) => $.detail.thread_nav_label)}
        onPointerMove={handleWaveMove}
        onPointerLeave={handleWaveLeave}
        onFocusCapture={handleFocus}
        onBlurCapture={scheduleClose}
        // Bounded height + shrinkable ticks: when threads outgrow the rail,
        // flex compresses the spacing (down to min-h) instead of overflowing.
        className="pointer-events-auto flex max-h-full flex-col overflow-hidden"
      >
        {threads.map((thread) => {
          const title = previews.get(thread.id)!.title;
          return (
            <MinimapTick
              key={thread.id}
              threadId={thread.id}
              // Announce resolution on the tick as well as in the outline.
              label={
                thread.resolved
                  ? t(($) => $.detail.thread_nav_resolved_label, { title })
                  : title
              }
              inViewport={visibleIds.has(thread.id)}
              isHighlighted={preview?.threadId === thread.id}
              isAgentMessage={knownA2AIntent(thread.entry.a2a_intent) !== null}
              onClick={(event) => handleJump(thread.id, event)}
            />
          );
        })}
      </nav>

      {preview && (
        <div
          ref={cardRef}
          onPointerEnter={cancelClose}
          onPointerLeave={scheduleClose}
          onFocusCapture={cancelClose}
          onBlurCapture={scheduleClose}
          onKeyDown={handleCardKeyDown}
          // The box no longer scrolls as a whole: the search band and the
          // pills stay put while only the list moves, so the field the reader
          // is typing into cannot scroll out from under them.
          className="pointer-events-auto absolute right-8 top-1/2 flex max-h-[calc(100%-3rem)] w-80 max-w-[calc(100vw-4rem)] -translate-y-1/2 flex-col overflow-hidden rounded-xl bg-popover text-body text-popover-foreground shadow-lg ring-1 ring-foreground/10"
        >
          <div className="flex h-10 shrink-0 items-center gap-2.5 px-3">
            <Search className="size-4 shrink-0 text-faint-foreground" aria-hidden />
            <input
              ref={searchRef}
              role="combobox"
              aria-expanded
              aria-controls={listId}
              aria-activedescendant={
                preview && rows.some((row) => row.id === preview.threadId)
                  ? optionId(preview.threadId)
                  : undefined
              }
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t(($) => $.detail.thread_outline.search_placeholder)}
              aria-label={t(($) => $.detail.thread_outline.search_placeholder)}
              className="min-w-0 flex-1 bg-transparent text-body text-foreground outline-none placeholder:text-faint-foreground"
            />
            {query.trim() !== "" && (
              <span className="shrink-0 text-caption tabular-nums text-faint-foreground">
                {t(($) => $.detail.thread_outline.match_count, { count: rows.length })}
              </span>
            )}
          </div>

          {/* Divider stays: it is the top edge of the scroll region, and
              without it the list slides under the pills with nothing marking
              the boundary. */}
          <div className="flex shrink-0 items-center gap-1 border-b border-border px-1.5 pb-2">
            {filterPills.map((pill) => (
              <button
                key={pill.id}
                type="button"
                onClick={() => setFilter(pill.id)}
                data-active={filter === pill.id || undefined}
                className={cn(
                  "flex h-7 items-center gap-1 rounded-full px-2 text-caption text-muted-foreground transition-colors",
                  "hover:bg-surface-hover",
                  "data-active:bg-surface-selected data-active:font-medium data-active:text-foreground data-active:hover:bg-surface-selected",
                )}
              >
                {/* The space is for the accessible name, not the layout —
                    without it the two spans concatenate to "Resolved1". */}
                {pill.label}{" "}
                <span className="tabular-nums text-faint-foreground">{pill.count}</span>
              </button>
            ))}
          </div>

          {rows.length === 0 ? (
            <p className="px-3 py-8 text-center text-caption text-muted-foreground">
              {t(($) => $.detail.thread_outline.empty)}
            </p>
          ) : (
          <ul
            ref={listRef}
            id={listId}
            className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-2"
          >
            {rows.map((thread) => {
              const title = previews.get(thread.id)!.title;
              const participantNames = thread.participants.map((participant) =>
                participant.actor_name || getActorName(participant.actor_type, participant.actor_id),
              );
              return (
                <li key={thread.id} data-thread-id={thread.id}>
                  <button
                    type="button"
                    id={optionId(thread.id)}
                    onPointerEnter={() => showPreview({ threadId: thread.id })}
                    onFocus={() => showPreview({ threadId: thread.id })}
                    onClick={(event) => handleJump(thread.id, event)}
                    data-active={preview.threadId === thread.id || undefined}
                    aria-label={thread.resolved
                      ? t(($) => $.detail.thread_nav_resolved_label, { title })
                      : title}
                    aria-description={participantNames.join(", ") || undefined}
                    className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-left text-body text-muted-foreground transition-colors hover:bg-surface-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring data-active:font-medium data-active:text-brand"
                  >
                    <span className="flex min-w-0 flex-1 items-center gap-1.5">
                      <span className="truncate">{highlightMatches(title, query)}</span>
                      {thread.resolved && (
                        <CheckCircle2
                          className="size-3.5 shrink-0 text-success"
                          aria-label={t(($) => $.comment.resolve.thread_resolved_badge)}
                        />
                      )}
                    </span>
                    <span className="inline-flex shrink-0 items-center -space-x-1.5" aria-hidden="true">
                      {thread.participants.slice(0, 3).map((participant, participantIndex) => {
                        const name = participantNames[participantIndex]!;
                        const avatarUrl = participant.actor_avatar_url?.startsWith("/")
                          ? resolvePublicFileUrl(participant.actor_avatar_url)
                          : participant.actor_avatar_url ?? getActorAvatarUrl(participant.actor_type, participant.actor_id);
                        return (
                          <span
                            key={`${participant.actor_type}:${participant.actor_id}`}
                            title={name}
                            className="inline-flex rounded-full ring-2 ring-popover"
                          >
                            <ActorAvatar
                              name={name}
                              initials={getActorInitials(participant.actor_type, participant.actor_id, name)}
                              avatarUrl={avatarUrl}
                              isAgent={participant.actor_type === "agent"}
                              size="sm"
                            />
                          </span>
                        );
                      })}
                      {thread.participants.length > 3 && (
                        <span
                          title={participantNames.slice(3).join(", ")}
                          className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-muted px-0.5 text-micro font-medium tabular-nums text-muted-foreground ring-2 ring-popover"
                        >
                          +{thread.participants.length - 3}
                        </span>
                      )}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          )}
        </div>
      )}
    </div>
  );
}
