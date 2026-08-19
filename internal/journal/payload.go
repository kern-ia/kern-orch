package journal

// Payload is one event's data. The unexported marker method restricts implementations to
// this package: a switch over Kind (see codec.go) is therefore exhaustive by construction —
// a new payload type added here without a matching case in kindOf or decodePayload is a
// compile-time or test-time failure, not a silent gap that swallows a new event kind.
type Payload interface {
	journalPayload()
}

// RunStarted opens a run's journal. Graph names the topology that was loaded — the same
// string the reporter's Topology carries, kept here independently rather than imported so
// this package never depends on internal/report (see the package doc for why).
type RunStarted struct {
	Graph string `json:"graph"`
}

// RunFinished closes a run that reached its end with no unrecovered error.
type RunFinished struct{}

// RunFailed closes a run that did not complete. Nodes lists every node of the failed level,
// mirroring internal/report's Failure shape by convention, not by import: a node absent
// from Nodes but present in the level's frontier completed, since the engine waits for the
// whole level before giving up.
type RunFailed struct {
	Message string   `json:"message"`
	Nodes   []string `json:"nodes,omitempty"`
}

// RunInterrupted closes a run's journal when the process stopped mid-level rather than
// finishing or failing cleanly. Emitting it explicitly is what lets resume distinguish "the
// last level never got a terminal event because the process died" from "the last level is
// still legitimately in flight" — an incomplete tail must be visibly closed, not inferred.
type RunInterrupted struct {
	Reason string `json:"reason,omitempty"`
}

// LevelOpened records the frontier about to run.
type LevelOpened struct {
	Frontier []string `json:"frontier"`
}

// CombinationRule names how a level's node branches were folded back into the shared state.
// It is data, not something replay can re-derive from the resulting keys: a single-node
// frontier and a fan-out that happens to touch the same keys leave an identical key set
// behind despite one replacing state and the other merging into it.
type CombinationRule string

const (
	// CombinationReplace is applied by a single-node frontier: the branch's state replaces
	// the shared state wholesale, so deletions and Freeze propagate. See graph.Engine.runLevel.
	CombinationReplace CombinationRule = "replace"
	// CombinationMerge is applied by a fan-out (more than one node in the frontier): each
	// branch's keys are overlaid additively onto the shared state. See graph.Engine.runLevel.
	CombinationMerge CombinationRule = "merge"
)

// LevelClosed records that a level finished and which CombinationRule folded its branches
// back into the shared state — the field the acceptance criteria call out by name, because
// without it replay has no way to tell a replace from a merge that happened to touch the
// same keys.
type LevelClosed struct {
	Frontier []string        `json:"frontier"`
	Rule     CombinationRule `json:"rule"`
}

// NodeStarted records that a node began executing within its level.
type NodeStarted struct {
	NodeID string `json:"node_id"`
}

// NodeProduced records the data a node wrote to its branch of the shared state. Zones
// mirrors graph.State's own convention (internal/graph/zones.go): only non-persistent keys
// appear, so a key absent from Zones is persistent, exactly as State.Zone treats it.
// Carrying zones here, not just key/value pairs, is what lets a projection honor a later
// Freeze's carry-over rule instead of guessing every key was persistent.
type NodeProduced struct {
	NodeID string            `json:"node_id"`
	Data   map[string]any    `json:"data,omitempty"`
	Zones  map[string]string `json:"zones,omitempty"`
}

// NodeFailed records that a node's execution returned an error.
type NodeFailed struct {
	NodeID  string `json:"node_id"`
	Message string `json:"message"`
}

// NudgeApplied records an out-of-band write to the shared state that did not come from any
// node. Origin names who or what applied it — required because, unlike a node's output, a
// nudge otherwise leaves no trace of its source; Data is the key/value pairs it applied.
type NudgeApplied struct {
	Origin string         `json:"origin"`
	Data   map[string]any `json:"data,omitempty"`
}

// FreezeApplied records a State.Freeze call. CarriedOver and Dropped are two independent
// fields, not one field plus its complement: State.Freeze replaces the state's contents
// wholesale with whatever the carry-over kept, so modelling this as key removals from the
// prior state would let replay reconstruct a state the run never had under any carry-over
// other than the default (see the epic's Notes on Freeze being the riskiest interaction).
type FreezeApplied struct {
	CarriedOver map[string]any `json:"carried_over,omitempty"`
	Dropped     []string       `json:"dropped,omitempty"`
}

func (RunStarted) journalPayload()     {}
func (RunFinished) journalPayload()    {}
func (RunFailed) journalPayload()      {}
func (RunInterrupted) journalPayload() {}
func (LevelOpened) journalPayload()    {}
func (LevelClosed) journalPayload()    {}
func (NodeStarted) journalPayload()    {}
func (NodeProduced) journalPayload()   {}
func (NodeFailed) journalPayload()     {}
func (NudgeApplied) journalPayload()   {}
func (FreezeApplied) journalPayload()  {}
