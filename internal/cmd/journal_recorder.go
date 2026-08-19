package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal"
)

// journalRecorder is the run's record: it turns what the engine reports into journal events
// and writes them, together with the projection row they derive, into one transaction.
//
// It exists in internal/cmd rather than in internal/checkpoint because it is the only place
// that holds all three things a journal.Event needs and the engine has none of — the run id,
// the graph's name and the sequence number. graph.EventFunc's own doc says so.
//
// # Why events are buffered rather than written as they arrive
//
// A level's events and the row that summarises it must land together, and the row is only
// knowable when the level closes: graph.StepInfo, which carries the step and the next
// frontier, exists only in the OnStep hook, which the engine calls after runLevel returned.
// Writing each event as it arrived would put the events of a level in the table before the
// row that closes it — precisely the "one observable without the other" the issue forbids.
//
// So level events accumulate, and the OnStep hook drains them into AppendAndProject. The
// run's own terminal events are the exception: they are emitted after the last hook has
// fired, with no row left to write, so they flush themselves through AppendAndReproject.
//
// # Concurrency
//
// graph.EventFunc requires an implementation safe for concurrent use — runLevel emits node
// events from each node's own goroutine. The mutex here covers both the pending slice and
// the sequence counter, because they must move together: two goroutines that read the same
// next-seq would hand the store a batch with a duplicate, which it refuses.
type journalRecorder struct {
	store *checkpoint.SQLiteStore
	runID string
	graph string
	now   func() time.Time

	mu      sync.Mutex
	nextSeq int64
	pending []journal.Event
}

// newJournalRecorder reads the run's stored next sequence number before recording anything.
//
// A fresh run answers FirstSeq, so the common case costs one query. A resumed run answers
// where its journal left off, which is the only reason this reads at all: a recorder that
// always started at FirstSeq would have every append on a resumed run refused with
// ErrSeqMismatch — an error the emitter could not have anticipated, on a path (`resume`) that
// works today. Replaying that tail correctly is issue 08; not corrupting it is this one's.
func newJournalRecorder(ctx context.Context, store *checkpoint.SQLiteStore, runID, graphName string) (*journalRecorder, error) {
	next, err := store.NextSeq(ctx, runID)
	if err != nil {
		return nil, err
	}
	return &journalRecorder{store: store, runID: runID, graph: graphName, now: time.Now, nextSeq: next}, nil
}

// record is the graph.EventFunc the engine reports through. It maps the event, buffers it,
// and — for a run's terminal events — writes what has accumulated straight away.
//
// Every error returned here aborts the run, which is graph.EventFunc's documented contract
// and the ordering multiStep already established: the durable record comes first, and a run
// whose record refused an event would carry on producing a journal that is silently
// incomplete.
func (r *journalRecorder) record(ctx context.Context, ev graph.Event) error {
	payload, err := journalPayload(ev, r.graph)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.pending = append(r.pending, journal.Event{
		RunID: r.runID, Seq: r.nextSeq, At: r.now().UTC(), Payload: payload,
	})
	r.nextSeq++
	r.mu.Unlock()

	if !closesTheRun(ev.Kind) {
		return nil
	}
	// No level is in flight when a run closes, so this drains from the run's own goroutine
	// with nothing to race — the lock inside take is for the node goroutines, not for here.
	batch := r.take()
	if err := r.store.AppendAndReproject(ctx, r.runID, batch...); err != nil {
		return err
	}
	return nil
}

// nudgeOrigin names where an out-of-band write came from, which journal.NudgeApplied
// requires. It is the surface, not a person: the steer mailbox checks the requester before
// queueing a nudge but does not carry it, so naming an actor here would mean inventing one.
const nudgeOrigin = "steer"

// recordNudge buffers the out-of-band write the steer mailbox applied between two levels.
// Nothing is recorded when nothing was applied — an empty nudge event on every level of
// every run would put a write in the record that never happened.
//
// This is nominally issue 06's event, and it is here because it has to be: the row stops
// being an independent write in this issue, and from that moment a state mutation the
// journal does not carry is a mutation the row loses. See the drift record for this branch.
// It is buffered rather than written on arrival so that it lands in the same transaction as
// the level it applies to — the live state and the record then fail together, which is what
// keeps them from disagreeing after a crash mid-level.
func (r *journalRecorder) recordNudge(_ context.Context, applied map[string]any) error {
	if len(applied) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, journal.Event{
		RunID: r.runID, Seq: r.nextSeq, At: r.now().UTC(),
		Payload: journal.NudgeApplied{Origin: nudgeOrigin, Data: copyData(applied)},
	})
	r.nextSeq++
	return nil
}

// checkpoint is the graph.StepFunc that closes a level: it takes the events that level
// produced and writes them with the row they derive, in one transaction. The *graph.State
// the engine passes is deliberately unused — it is the live state, and a row built from it
// would be an independent write that merely looks derived (see checkpoint.Projection).
func (r *journalRecorder) checkpoint(graphPath, requester, dossier string) graph.StepFunc {
	return func(ctx context.Context, info graph.StepInfo, _ *graph.State) error {
		status := checkpoint.StatusRunning
		if len(info.Frontier) == 0 {
			status = checkpoint.StatusDone
		}
		return r.store.AppendAndProject(ctx, checkpoint.Projection{
			RunID: r.runID, Step: info.Step, Frontier: info.Frontier, Status: status,
			GraphPath: graphPath, Requester: requester, Dossier: dossier,
		}, r.take()...)
	}
}

// take drains the buffered events. It hands the slice over rather than copying it and then
// clearing: a batch the store refuses is a batch that must not be silently retried on the
// next level, because the sequence numbers it carries are already spent.
func (r *journalRecorder) take() []journal.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	batch := r.pending
	r.pending = nil
	return batch
}

// closesTheRun reports the kinds emitted outside any level, after the last OnStep hook has
// fired. They have no row of their own to wait for, so they are written on arrival.
func closesTheRun(kind graph.EventKind) bool {
	switch kind {
	case graph.EventRunStarted, graph.EventRunFinished, graph.EventRunFailed:
		return true
	default:
		return false
	}
}

// journalPayload is the adapter between the two vocabularies: graph's, declared over types
// the engine owns, and the journal's. They mirror each other name for name on purpose (see
// graph.EventFunc's doc on the dependency direction), so this is one flat switch — and it is
// the single place a divergence between them can be caught.
//
// The three fields the engine cannot know — the run id, the sequence number and the graph's
// name — are the caller's; only the graph name reaches a payload, and it is passed in.
//
// An unknown kind is refused rather than skipped. A hook error aborts the run, which is the
// harsher of the two outcomes and the right one: an event dropped here leaves a journal that
// replays into a state the run never had, and nothing downstream could tell.
func journalPayload(ev graph.Event, graphName string) (journal.Payload, error) {
	switch ev.Kind {
	case graph.EventRunStarted:
		return journal.RunStarted{Graph: graphName}, nil
	case graph.EventRunFinished:
		return journal.RunFinished{}, nil
	case graph.EventRunFailed:
		return journal.RunFailed{Message: errorText(ev.Err), Nodes: copyStrings(ev.Nodes)}, nil
	case graph.EventLevelOpened:
		return journal.LevelOpened{Frontier: copyStrings(ev.Frontier)}, nil
	case graph.EventLevelClosed:
		rule, err := journalRule(ev.Rule)
		if err != nil {
			return nil, err
		}
		return journal.LevelClosed{Frontier: copyStrings(ev.Frontier), Rule: rule}, nil
	case graph.EventNodeStarted:
		return journal.NodeStarted{NodeID: ev.NodeID}, nil
	case graph.EventNodeProduced:
		return journal.NodeProduced{
			NodeID: ev.NodeID, Data: copyData(ev.Data), Zones: copyZones(ev.Zones),
		}, nil
	case graph.EventNodeFailed:
		return journal.NodeFailed{NodeID: ev.NodeID, Message: errorText(ev.Err)}, nil
	default:
		return nil, fmt.Errorf("cmd: record run event: unknown event kind %q", string(ev.Kind))
	}
}

// journalRule maps the combination rule across, by an explicit switch rather than a string
// conversion. The two constants happen to share their text today, and a conversion would
// keep compiling — silently writing an unknown rule into the journal — the day one of them
// gains a value the other does not have.
func journalRule(rule graph.CombinationRule) (journal.CombinationRule, error) {
	switch rule {
	case graph.CombinationReplace:
		return journal.CombinationReplace, nil
	case graph.CombinationMerge:
		return journal.CombinationMerge, nil
	default:
		return "", fmt.Errorf("cmd: record level closed: unknown combination rule %q", string(rule))
	}
}

// errorText renders an event's error for a payload that carries a message rather than an
// error. A nil error becomes the empty string: the engine only sets Err on the kinds that
// have one, and inventing text for the others would put a failure in the record.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// copyStrings, copyData and copyZones detach the payload from the engine's own maps and
// slices. Events are buffered until their level closes, and the engine reuses its frontier
// slice across levels — a payload holding a reference would have an already-recorded event
// rewritten by a later one. nil in, nil out, so an absent field stays absent on the wire.
func copyStrings(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func copyData(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func copyZones(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
