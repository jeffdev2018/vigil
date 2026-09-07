import { z } from "zod";

// Run previews (F12). A run in worktree mode can bring up the app it is
// working on; this is how a client reads where it is and shares it.
//
// `status` and `scheme` are z.string(), not enums. An installed desktop client
// talking to a newer backend must be able to READ a state it does not know
// without the whole panel falling over — and, just as importantly, must not
// render a link for it. Everything that decides "can this be opened" goes
// through isPreviewOpenable, which tests for the one status that is openable
// rather than excluding the ones that are not.

export const RUN_PREVIEW_READY = "ready";

export const RunPreviewSchema = z
  .object({
    status: z.string().catch(""),
    scheme: z.string().catch(""),
    url: z.string().catch(""),
    port: z.number().catch(0),
    expires_at: z.string().nullish().catch(null),
    error: z.string().catch(""),
    relay_available: z.boolean().catch(false),
  })
  .loose();
export type RunPreview = z.infer<typeof RunPreviewSchema>;

/**
 * The fallback for an unreadable response is "no preview". Rendering a chip
 * from a response that could not be parsed would claim a run has a dev server
 * running when nothing knows whether it does.
 */
export const EMPTY_RUN_PREVIEW: RunPreview = {
  status: "",
  scheme: "",
  url: "",
  port: 0,
  expires_at: null,
  error: "",
  relay_available: false,
};

export const TaskShareLinkSchema = z
  .object({
    id: z.string(),
    code: z.string().catch(""),
    url: z.string().catch(""),
    capabilities: z.array(z.string()).catch([]).default([]),
    expires_at: z.string().catch(""),
    created_at: z.string().catch(""),
    use_count: z.number().catch(0),
  })
  .loose();
export type TaskShareLink = z.infer<typeof TaskShareLinkSchema>;

export const TaskShareLinkListSchema = z
  .object({ links: z.array(TaskShareLinkSchema).catch([]).default([]) })
  .loose();
export type TaskShareLinkList = z.infer<typeof TaskShareLinkListSchema>;

export const EMPTY_TASK_SHARE_LINKS: TaskShareLinkList = { links: [] };

/**
 * Whether this preview can be opened right now.
 *
 * `ready` is tested for explicitly. A status this build does not recognise —
 * a state a newer server added — is not openable, because the alternative is
 * handing a reviewer a link that fails in their hands.
 */
export function isPreviewOpenable(preview: RunPreview | undefined): boolean {
  return preview?.status === RUN_PREVIEW_READY && !!preview.url;
}

/**
 * A loopback preview is only reachable on the machine that ran the task. The
 * desktop app can open it; the web app must say so instead of linking it.
 */
export function isPreviewLocalOnly(preview: RunPreview | undefined): boolean {
  return preview?.status === RUN_PREVIEW_READY && preview.scheme === "loopback";
}

/**
 * A relayed preview that is ready but has no URL is one nobody has shared yet.
 * That is the state the "create a link" affordance exists for.
 */
export function previewNeedsShareLink(preview: RunPreview | undefined): boolean {
  return preview?.status === RUN_PREVIEW_READY && preview.scheme === "relay" && !preview.url;
}

/** Middle-ellipsis so both the host and the path stay legible in a chip. */
export function truncatePreviewUrl(url: string, max = 48): string {
  if (url.length <= max) return url;
  const head = Math.ceil((max - 1) / 2);
  const tail = Math.floor((max - 1) / 2);
  return `${url.slice(0, head)}…${url.slice(url.length - tail)}`;
}


/** The fallback for an unreadable single-link response. */
export const EMPTY_TASK_SHARE_LINK: TaskShareLink = {
  id: "",
  code: "",
  url: "",
  capabilities: [],
  expires_at: "",
  created_at: "",
  use_count: 0,
};

export interface CreateShareLinkInput {
  capabilities?: string[];
  expires_in_hours?: number;
}
