// Package checkpoint persists run state per step to SQLite so a run can be resumed
// after failure and inspected. It depends on graph only for the State type; the graph
// engine reaches this package through its StepFunc hook, not the other way around.
package checkpoint

import (
	"context"
	"errors"
	"time"

	"github.com/yoann/kern-orch/internal/graph"
)

// Run status values persisted with each checkpoint.
const (
	// StatusQueued marks a run accepted but not yet at its first completed level — the
	// daemon writes it the instant a run is accepted, so a status query right after
	// acceptance finds something rather than racing the engine's first checkpoint.
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// QueuedStep is the step a queued marker is written at. Negative and below any step the
// engine itself ever reports (which starts at 0 and only grows), so Latest/List — both
// keyed on MAX(step) — pick the real checkpoint the moment one lands, and the marker only
// once nothing else exists.
const QueuedStep = -1

// ErrEmptyRunID is returned when a Record has no run id.
var ErrEmptyRunID = errors.New("checkpoint: empty run id")

// Record is one persisted checkpoint: the state after a level, plus the frontier still
// to execute (empty when finished), keyed by (RunID, Step).
type Record struct {
	RunID     string
	Step      int
	Frontier  []string
	State     *graph.State
	Status    string
	CreatedAt time.Time
	// GraphPath is the source graph file the run was launched from, so `resume` can
	// reload it without the caller re-supplying the path.
	GraphPath string
	// Requester names who asked for this run. Empty means open — steerable by anyone,
	// which is what every CLI-started run is today. C6's write path (stop/nudge/decide)
	// checks this against the caller before acting.
	Requester string
	// Dossier is a caller-supplied business label (e.g. a client case) grouping several
	// runs together for a consumer like kern-ui's dossiers list. Distinct from Requester:
	// one is an identity used for a permission check, the other a grouping key with no
	// bearing on who may steer the run. Empty means the run belongs to no dossier.
	Dossier string
}

// Summary is a per-run rollup for the `status` command.
type Summary struct {
	RunID     string
	LastStep  int
	Status    string
	UpdatedAt time.Time
}

// Store persists and retrieves checkpoints. Implementations must be safe for use from
// a single run's goroutine; the engine calls Save sequentially between levels.
type Store interface {
	Save(ctx context.Context, r Record) error
	Latest(ctx context.Context, runID string) (Record, bool, error)
	List(ctx context.Context) ([]Summary, error)
	Close() error
}
