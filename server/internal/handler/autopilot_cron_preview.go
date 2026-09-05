package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

// Rejection codes let the editor say which input is at fault: a timezone the
// server's tzdata does not know is not the user's cron being wrong, and the two
// are fixed in different controls.
const (
	cronPreviewInvalidCron     = "invalid_cron"
	cronPreviewInvalidTimezone = "invalid_timezone"
	cronPreviewInvalidWindow   = "invalid_window_minutes"
)

// previewTriggerID is the identity WindowOffset hashes for a preview. A
// candidate schedule has no trigger row yet, so the preview can only show a
// representative minute inside the band — the band itself is what the editor is
// asking about, and the saved trigger will pick its own minute from its own id.
const previewTriggerID = ""

func writeCronPreviewError(w http.ResponseWriter, code, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg, "code": code})
}

// CronPreview computes the next occurrences of a candidate cron expression
// so schedule editors can show an authoritative preview before saving.
// Compute-only: workspace membership is enforced by the router group and no
// other resource is touched.
func (h *Handler) CronPreview(w http.ResponseWriter, r *http.Request) {
	expr := r.URL.Query().Get("expr")
	if expr == "" {
		writeCronPreviewError(w, cronPreviewInvalidCron, "expr is required")
		return
	}
	tz := r.URL.Query().Get("tz")
	if tz == "" {
		tz = "UTC"
	}
	if err := service.ValidateTimezone(tz); err != nil {
		writeCronPreviewError(w, cronPreviewInvalidTimezone, err.Error())
		return
	}

	windowMinutes, ok := parseCronPreviewWindow(w, r.URL.Query().Get("window_minutes"))
	if !ok {
		return
	}

	const previewCount = 3
	// A syntactically valid expression that never fires comes back as a short (or
	// empty) slice, not an error: the editor tells "never runs" from "your cron is
	// wrong" by the status code.
	occurrences, err := nextPreviewOccurrences(expr, tz, windowMinutes, previewCount)
	if err != nil {
		writeCronPreviewError(w, cronPreviewInvalidCron, err.Error())
		return
	}
	nextRuns := make([]string, 0, len(occurrences))
	for _, at := range occurrences {
		nextRuns = append(nextRuns, at.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"next_runs": nextRuns})
}

// parseCronPreviewWindow reads the optional window_minutes parameter under the
// same bounds the trigger write paths enforce, so the preview cannot promise a
// band the PATCH would refuse.
func parseCronPreviewWindow(w http.ResponseWriter, raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		writeCronPreviewError(w, cronPreviewInvalidWindow, "window_minutes must be an integer")
		return 0, false
	}
	if err := validateWindowMinutes(n); err != nil {
		writeCronPreviewError(w, cronPreviewInvalidWindow, err.Error())
		return 0, false
	}
	return n, true
}

// nextPreviewOccurrences returns the next `count` firing instants. With a band
// the instants are the band starts shifted by WindowOffset, which is what the
// scheduler will dispatch on — a preview showing the band start would name a
// minute the trigger never fires at.
func nextPreviewOccurrences(expr, tz string, windowMinutes, count int) ([]time.Time, error) {
	now := time.Now().UTC()
	if windowMinutes <= 0 {
		return service.NextOccurrencesAfterUTC(expr, tz, now, count)
	}
	// Parse once through the unwindowed helper first: it is the only one that
	// tells "this cron is wrong" (an error) from "this cron has no further
	// occurrence" (a short slice). The windowed search reports both as errors,
	// and an expression that never fires again must stay a 200 with an empty
	// list, exactly as it is without a band.
	if _, err := service.NextOccurrencesAfterUTC(expr, tz, now, 1); err != nil {
		return nil, err
	}
	window := time.Duration(windowMinutes) * time.Minute
	out := make([]time.Time, 0, count)
	cursor := now
	for i := 0; i < count; i++ {
		next, err := service.NextWindowedOccurrenceAfterUTC(expr, tz, window, previewTriggerID, cursor)
		if err != nil {
			break
		}
		out = append(out, next)
		cursor = next
	}
	return out, nil
}
