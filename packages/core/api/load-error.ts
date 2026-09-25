// isResourceMissingError tells a page whether a failed read is the server's
// answer about the resource — a 4xx: it does not exist, or this caller may not
// see it — or no answer at all: a transport failure (offline, aborted), a 5xx,
// a timeout or a rate limit. After the latter the resource may well exist, so
// the page must offer a retry instead of saying it was deleted.
//
// Duck-typed on `status` rather than `instanceof ApiError` so it holds for any
// error carrying an HTTP status, and stays importable without the API client.
export function isResourceMissingError(error: unknown): boolean {
  if (typeof error !== "object" || error === null || !("status" in error)) {
    return false;
  }
  const status = (error as { status?: unknown }).status;
  return (
    typeof status === "number" &&
    status >= 400 &&
    status < 500 &&
    status !== 408 &&
    status !== 429
  );
}
