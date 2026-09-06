package service

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func mirrorTestUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var out pgtype.UUID
	if err := out.Scan(uuid.NewString()); err != nil {
		t.Fatalf("scan uuid: %v", err)
	}
	return out
}

func mirrorLink(source, target pgtype.UUID) db.ProjectMirrorLink {
	return db.ProjectMirrorLink{SourceProjectID: source, TargetProjectID: target, TriggerLabel: "mirror"}
}

// The cycle guard is asked "does the proposed TARGET already reach the
// proposed SOURCE?" — every case below is phrased that way.
func TestMirrorGraphReaches(t *testing.T) {
	a, b, c, d := mirrorTestUUID(t), mirrorTestUUID(t), mirrorTestUUID(t), mirrorTestUUID(t)

	cases := []struct {
		name  string
		links []db.ProjectMirrorLink
		from  pgtype.UUID
		want  pgtype.UUID
		reach bool
	}{
		{"empty graph reaches nothing", nil, b, a, false},
		{"same node is reached at depth zero", nil, a, a, true},
		{"direct back edge closes a 2-cycle", []db.ProjectMirrorLink{mirrorLink(b, a)}, b, a, true},
		{"transitive back edge closes a 3-cycle", []db.ProjectMirrorLink{mirrorLink(b, c), mirrorLink(c, a)}, b, a, true},
		{"forward-only chain is not a cycle", []db.ProjectMirrorLink{mirrorLink(a, b), mirrorLink(b, c)}, c, d, false},
		{"a diamond is not a cycle", []db.ProjectMirrorLink{mirrorLink(a, b), mirrorLink(a, c), mirrorLink(b, d), mirrorLink(c, d)}, d, a, false},
		// A graph that is already cyclic (rows written before this guard, or by
		// hand) must terminate rather than spin: the visited set is what stops it.
		{"pre-existing cycle terminates", []db.ProjectMirrorLink{mirrorLink(b, c), mirrorLink(c, b)}, b, a, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mirrorGraphReaches(tc.links, tc.from, tc.want); got != tc.reach {
				t.Fatalf("mirrorGraphReaches = %v, want %v", got, tc.reach)
			}
		})
	}
}

func TestMirrorDescriptionCarriesProvenanceThenSource(t *testing.T) {
	got := mirrorDescription("JIA-7", "Backend", pgtype.Text{String: "fix the thing", Valid: true})
	if want := "Generated from JIA-7 (Backend)\n\nfix the thing"; got != want {
		t.Fatalf("mirrorDescription = %q, want %q", got, want)
	}
	// A source with no project title must not leave empty parentheses behind.
	if got := mirrorDescription("JIA-7", "", pgtype.Text{}); got != "Generated from JIA-7\n\n" {
		t.Fatalf("mirrorDescription without project = %q", got)
	}
}
