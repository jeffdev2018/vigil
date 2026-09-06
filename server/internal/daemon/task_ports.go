package daemon

import (
	"fmt"
	"net"
	"time"
)

// Per-run port blocks (F09).
//
// The slot a run holds is stable in [0, MaxConcurrentTasks) and unique among
// LIVE runs, which is exactly the property a port offset needs: two runs
// overlapping in time cannot share one. slot × count from a configured base
// gives each run its own contiguous range, exported as MULTICA_PORT_BASE and
// MULTICA_PORT_COUNT for the agent and the lifecycle scripts to bind inside.
//
// The slot alone is not enough on a real machine. It is unique within ONE
// daemon; a second profile, a second checkout, or any unrelated process may
// already hold the block. So the base is probed before it is handed out and
// shifted by a whole block while it is busy — a whole block, never one port, so
// two runs can never end up with interleaved ranges.

const (
	// taskPortProbeTimeout bounds one listen attempt. Local only, so this is
	// generous; the whole probe is at most taskPortProbeAttempts of these.
	taskPortProbeTimeout = 150 * time.Millisecond
	// taskPortProbeAttempts bounds the shifting. Past it the run gets the block
	// its slot names anyway: a busy port is a problem the run may survive,
	// while refusing to start is one it certainly does not.
	taskPortProbeAttempts = 16
)

// resolveTaskPortBase returns the first port of this slot's block, shifted past
// any block whose first port is already bound.
func resolveTaskPortBase(base, count, slot int) int {
	start := base + slot*count
	for attempt := 0; attempt < taskPortProbeAttempts; attempt++ {
		candidate := start + attempt*count*maxProbeStride
		if candidate+count > 65535 {
			break
		}
		if taskPortFree(candidate) {
			return candidate
		}
	}
	return start
}

// maxProbeStride keeps a shifted block clear of every other slot's block. A
// shift of one block would land on the neighbouring slot's range; a shift of a
// full concurrency width cannot.
//
// ponytail: a constant rather than the live MaxConcurrentTasks — the daemon
// default is 20 and reading the config here would thread it through four call
// sites for a probe that almost never fires. Raise it if a deployment runs more
// than 64 concurrent tasks.
const maxProbeStride = 64

// taskPortFree reports whether a port can be bound right now. A false negative
// (something binds it a moment later) costs the run the same collision it would
// have had; there is no lease to hold, because the ports are for the agent to
// bind, not for us.
func taskPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
