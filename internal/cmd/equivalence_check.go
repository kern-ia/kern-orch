package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal/projection"
)

// equivalenceProject is the seam issue 10's projection call goes through here. Production
// wiring never overrides it; a test does, wrapping it with a counter, so "the disabled check
// adds no projection work" can be shown by a call count rather than by timing — timing a fast
// operation is inherently noisy, a call count is not.
var equivalenceProject = projection.Project

// equivalenceCheckHook builds issue 13's opt-in runtime check: at every level boundary it
// replays the journal checkpointHook just wrote for this run and compares the result to the
// live state the engine is carrying forward, using the exact comparison issue 10 proved
// correct over real engine runs (assertReplayEquivalent in replay_equivalence_test.go —
// full-state JSON encoding via graph.State.MarshalJSON, both sides round-tripped through
// JSON so an int a node wrote compares equal to the float64 the journal hands back). This
// function only wires that comparison into the running hook chain and adds naming the
// divergent keys on top; it does not reimplement the comparison itself, which is out of this
// issue's scope.
//
// enabled is cfg.RuntimeEquivalenceCheck, passed in rather than the whole Config so the
// caller states plainly what this depends on. When false it returns nil, and multiStep skips
// a nil hook entirely — so the disabled state adds no per-level call at all, not merely a
// cheap one, satisfying the "no additional projection work" half of this issue on its own
// without a runtime branch inside a hook that always runs.
func equivalenceCheckHook(enabled bool, store *checkpoint.SQLiteStore, runID string) graph.StepFunc {
	if !enabled {
		return nil
	}
	return func(ctx context.Context, info graph.StepInfo, live *graph.State) error {
		events, err := store.Read(ctx, runID)
		if err != nil {
			return fmt.Errorf("cmd: runtime equivalence check: run %s: read journal: %w", runID, err)
		}
		projected, err := equivalenceProject(events)
		if err != nil {
			return fmt.Errorf("cmd: runtime equivalence check: run %s: project journal: %w", runID, err)
		}
		diverged, err := divergentStateKeys(projected, live)
		if err != nil {
			return fmt.Errorf("cmd: runtime equivalence check: run %s: compare states: %w", runID, err)
		}
		if len(diverged) > 0 {
			return fmt.Errorf("cmd: runtime equivalence check: run %s level %d: projection diverged from live state at %s",
				runID, info.Step, strings.Join(diverged, ", "))
		}
		return nil
	}
}

// stateWireView mirrors the JSON shape graph.State.MarshalJSON documents
// ({"step":N,"frozen":N,"data":{...},"zones":{...}}), decoded generically so individual keys
// can be named instead of only the fact that the two full encodings differ. graph.State's own
// wire struct is unexported, so this is a second, read-only view of the same documented
// shape — not a second serialization, since both sides are still produced by
// json.Marshal(*graph.State) itself.
type stateWireView struct {
	Step   int                        `json:"step"`
	Frozen int                        `json:"frozen"`
	Data   map[string]json.RawMessage `json:"data"`
	Zones  map[string]string          `json:"zones"`
}

// divergentStateKeys compares two states through the same full-state JSON encoding issue 10's
// assertReplayEquivalent uses, then — unlike that test helper, which only fails the test —
// names what differs, since a runtime caller has no test failure printout to read. A nil
// slice with a nil error means the two states are equivalent.
func divergentStateKeys(projected, live *graph.State) ([]string, error) {
	pb, err := json.Marshal(projected)
	if err != nil {
		return nil, fmt.Errorf("marshal projected state: %w", err)
	}
	lb, err := json.Marshal(live)
	if err != nil {
		return nil, fmt.Errorf("marshal live state: %w", err)
	}
	if string(pb) == string(lb) {
		return nil, nil
	}

	var pw, lw stateWireView
	if err := json.Unmarshal(pb, &pw); err != nil {
		return nil, fmt.Errorf("decode projected state: %w", err)
	}
	if err := json.Unmarshal(lb, &lw); err != nil {
		return nil, fmt.Errorf("decode live state: %w", err)
	}

	var diffs []string
	if pw.Step != lw.Step {
		diffs = append(diffs, "step")
	}
	if pw.Frozen != lw.Frozen {
		diffs = append(diffs, "frozen")
	}
	for k := range unionKeys(pw.Data, lw.Data) {
		if string(pw.Data[k]) != string(lw.Data[k]) {
			diffs = append(diffs, "data."+k)
		}
	}
	for k := range unionKeys(pw.Zones, lw.Zones) {
		if pw.Zones[k] != lw.Zones[k] {
			diffs = append(diffs, "zones."+k)
		}
	}
	sort.Strings(diffs)
	return diffs, nil
}

// unionKeys returns the set of keys present in either map, so divergentStateKeys reports a
// key that only one side holds (not just one whose value changed) as a divergence too.
func unionKeys[K comparable, V any](a, b map[K]V) map[K]struct{} {
	keys := make(map[K]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	return keys
}
