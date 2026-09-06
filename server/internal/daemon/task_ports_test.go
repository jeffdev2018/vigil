package daemon

import (
	"fmt"
	"net"
	"strconv"
	"testing"
)

// F09: two runs overlapping in time must never be handed the same ports. The
// slot is unique among live runs, so the block derived from it is too.
func TestTaskPortBlocksAreDisjointAcrossSlots(t *testing.T) {
	const base, count = 21000, 10

	seen := map[int]int{}
	for slot := 0; slot < 4; slot++ {
		env := taskMulticaEnvironment(Task{ID: fmt.Sprintf("task-%d", slot)}, "agent", "token",
			"/cfg", "/ws", "https://example", 19514, slot, "/tmp", base, count)

		got, err := strconv.Atoi(env["MULTICA_PORT_BASE"])
		if err != nil {
			t.Fatalf("slot %d: MULTICA_PORT_BASE = %q: %v", slot, env["MULTICA_PORT_BASE"], err)
		}
		if env["MULTICA_PORT_COUNT"] != strconv.Itoa(count) {
			t.Fatalf("slot %d: MULTICA_PORT_COUNT = %q, want %d", slot, env["MULTICA_PORT_COUNT"], count)
		}
		for port := got; port < got+count; port++ {
			if other, clash := seen[port]; clash {
				t.Fatalf("slot %d and slot %d both own port %d", slot, other, port)
			}
			seen[port] = slot
		}
	}
}

// A daemon whose port block is not configured must export nothing rather than
// a synthesised default: an absent variable is what every wrapper predating the
// feature already handles.
func TestTaskPortBlockAbsentWhenNotConfigured(t *testing.T) {
	env := taskMulticaEnvironment(Task{ID: "task"}, "agent", "token",
		"/cfg", "/ws", "https://example", 19514, 0, "/tmp", 0, 0)
	if _, ok := env["MULTICA_PORT_BASE"]; ok {
		t.Fatalf("MULTICA_PORT_BASE was exported with no port block configured: %q", env["MULTICA_PORT_BASE"])
	}
	if _, ok := env["MULTICA_PORT_COUNT"]; ok {
		t.Fatal("MULTICA_PORT_COUNT was exported with no port block configured")
	}
}

// The slot is unique inside ONE daemon. A second profile, a second checkout or
// an unrelated process may already hold the block, so a bound base shifts the
// whole run rather than colliding.
func TestTaskPortBaseShiftsPastABusyBlock(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a local port: %v", err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port

	const count = 10
	got := resolveTaskPortBase(busy, count, 0)
	if got == busy {
		t.Fatalf("resolveTaskPortBase returned the busy base %d", busy)
	}
	if (got-busy)%(count*maxProbeStride) != 0 {
		t.Fatalf("shifted base %d is not a whole number of blocks past %d — two runs could interleave", got, busy)
	}
}
