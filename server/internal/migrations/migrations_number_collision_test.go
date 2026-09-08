package migrations

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// grandfatheredDuplicateNumbers records every migration number that already
// carries more than one migration. They are harmless where they sit: Files()
// sorts on the whole filename, not the number, so the order is deterministic,
// and schema_migrations keys on the full version stem, so two files sharing a
// number are two independent ledger rows. Renaming them now would orphan those
// rows on every deployed database, which is why they are frozen rather than
// fixed.
//
// The number is still the thing a human reads when picking "the next free
// one", and a fresh collision is a real hazard: two authors on two branches
// both take 837, both merge, and the pair silently applies in alphabetical
// order rather than the order either of them reasoned about. This guard makes
// that impossible to merge by accident. Adding a number here requires
// justifying why a new collision is acceptable — the answer is normally to
// renumber instead.
var grandfatheredDuplicateNumbers = map[string]bool{
	"020": true, "026": true, "029": true, "032": true, "033": true,
	"035": true, "040": true, "041": true, "043": true, "046": true,
	"050": true, "060": true, "065": true, "069": true, "079": true,
	"083": true, "084": true, "091": true, "095": true, "096": true,
	"098": true, "109": true, "111": true, "112": true, "113": true,
	"120": true, "122": true, "124": true, "127": true, "128": true,
	"545": true,
}

// TestNoNewMigrationNumberCollisions fails when a migration number gains a
// second migration outside the grandfathered set, and equally when a
// grandfathered number stops colliding — a stale entry would quietly re-open
// the door it was added to close.
func TestNoNewMigrationNumberCollisions(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")

	stemsByNumber := make(map[string][]string)
	for _, file := range files {
		stem, _, ok := splitMigrationFilename(filepath.Base(file))
		if !ok {
			continue
		}
		number, _, found := strings.Cut(stem, "_")
		if !found {
			t.Errorf("migration %s has no NNN_ prefix", stem)
			continue
		}
		stemsByNumber[number] = append(stemsByNumber[number], stem)
	}

	var unexpected, stale []string
	for number, stems := range stemsByNumber {
		collides := len(stems) > 1
		switch {
		case collides && !grandfatheredDuplicateNumbers[number]:
			sort.Strings(stems)
			unexpected = append(unexpected,
				number+" ("+strings.Join(stems, ", ")+")")
		case !collides && grandfatheredDuplicateNumbers[number]:
			stale = append(stale, number)
		}
	}
	for number := range grandfatheredDuplicateNumbers {
		if _, ok := stemsByNumber[number]; !ok {
			stale = append(stale, number)
		}
	}
	sort.Strings(unexpected)
	sort.Strings(stale)

	if len(unexpected) > 0 {
		t.Errorf("new migration number collision(s): %s\n"+
			"Pick the next free number instead of reusing one; the ordering "+
			"between two files sharing a number is alphabetical, not authored.",
			strings.Join(unexpected, "; "))
	}
	if len(stale) > 0 {
		t.Errorf("grandfatheredDuplicateNumbers lists number(s) that no longer "+
			"collide: %s. Remove them so the guard keeps its meaning.",
			strings.Join(stale, ", "))
	}
}
