// Package journal declares kern-orch's own internal vocabulary for the record of a run —
// deliberately distinct from the kern.step-event wire contract in internal/report.
//
// The wire contract cannot double as the durable record: internal/report.StepEvent's own
// comment says marshalling graph.State itself "would ship kern-orch's envelope ... across
// the contract, which is nobody else's business", so it flattens state to business data and
// drops zones, the frozen counter and per-node attribution on the way out. Replay needs
// exactly those fields back — which combination rule folded a level's branches, what a
// Freeze carried over versus dropped, who applied a nudge. A vocabulary rich enough for
// replay and a wire format that deliberately stays thin cannot be the same type without one
// of the two obligations losing.
//
// This package therefore imports nothing from internal/report, and internal/report imports
// nothing from it (verified in event_test.go): kern.step-event/v1 and /v2 stay a projection
// of the journal, produced elsewhere (issue 04 of this epic), not the journal itself.
package journal

import (
	"encoding/json"
	"fmt"
	"time"
)

// Kind names one of the closed set of event types this package declares. It is the
// discriminator the JSON codec switches on; see kindOf and decodePayload in codec.go.
type Kind string

const (
	KindRunStarted     Kind = "run_started"
	KindRunFinished    Kind = "run_finished"
	KindRunFailed      Kind = "run_failed"
	KindRunInterrupted Kind = "run_interrupted"
	KindLevelOpened    Kind = "level_opened"
	KindLevelClosed    Kind = "level_closed"
	KindNodeStarted    Kind = "node_started"
	KindNodeProduced   Kind = "node_produced"
	KindNodeFailed     Kind = "node_failed"
	KindNudgeApplied   Kind = "nudge_applied"
	KindFreezeApplied  Kind = "freeze_applied"
)

// Event is one entry in a run's journal: the run it belongs to, its position in that run's
// monotonic sequence, when it happened, and its payload. Seq is assigned and enforced
// monotonic by whoever appends to the journal (internal/checkpoint, issue 03 of this epic);
// this package only carries the field.
type Event struct {
	RunID string    `json:"run_id"`
	Seq   int64     `json:"seq"`
	At    time.Time `json:"at"`
	// Synthetic marks an event nobody observed: it was reconstructed after the fact, by a
	// reader closing a record the writer never got to close (internal/checkpoint, issue 12).
	// It is provenance, not content, which is why it sits on the envelope rather than in a
	// field of every payload that could be reconstructed — and why it is one flag rather
	// than a second Kind: a synthetic RunInterrupted says the same thing about the run as a
	// real one would, only nobody was alive to say it.
	//
	// At carries the moment the event was written, not the moment it describes, and for a
	// synthetic event those are far apart. The marking is what tells a reader so.
	Synthetic bool    `json:"synthetic,omitempty"`
	Payload   Payload `json:"payload"`
}

// eventWire is Event's JSON shape: Kind rides alongside Payload as an explicit discriminator
// rather than being inferred from Payload's Go type, because the inverse direction —
// decoding — has no Go type to infer from, only the bytes on the wire.
type eventWire struct {
	RunID string    `json:"run_id"`
	Seq   int64     `json:"seq"`
	Kind  Kind      `json:"kind"`
	At    time.Time `json:"at"`
	// omitempty, so an observed event encodes to exactly the bytes it did before this
	// field existed. Every journal already stored was written by an observer, and a
	// reader must be able to compare an untouched run's rows byte for byte.
	Synthetic bool            `json:"synthetic,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// MarshalJSON encodes e with its Kind derived from Payload's concrete type via kindOf, so
// the two can never disagree on the wire the way they could if Kind were a separate field
// the caller had to set correctly by hand.
func (e Event) MarshalJSON() ([]byte, error) {
	kind, err := kindOf(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("journal: marshal event: %w", err)
	}
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, fmt.Errorf("journal: marshal %s payload: %w", kind, err)
	}
	return json.Marshal(eventWire{
		RunID:     e.RunID,
		Seq:       e.Seq,
		Kind:      kind,
		At:        e.At,
		Synthetic: e.Synthetic,
		Payload:   payload,
	})
}

// UnmarshalJSON decodes e, failing loud when Kind is missing or names a type this package
// does not declare — a skipped or silently-nil payload here would let a future event type
// pass through replay unnoticed instead of stopping it.
func (e *Event) UnmarshalJSON(b []byte) error {
	var raw eventWire
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("journal: unmarshal event: %w", err)
	}
	if raw.Kind == "" {
		return fmt.Errorf("journal: unmarshal event: missing kind")
	}
	payload, err := decodePayload(raw.Kind, raw.Payload)
	if err != nil {
		return fmt.Errorf("journal: unmarshal event: %w", err)
	}
	e.RunID = raw.RunID
	e.Seq = raw.Seq
	e.At = raw.At
	e.Synthetic = raw.Synthetic
	e.Payload = payload
	return nil
}

// kindOf maps a Payload to its Kind. The switch has no default case returning a zero value:
// an unhandled Payload implementation falls through to the explicit error below, so adding a
// new type to payload.go without adding it here is caught the first time anything encodes
// one, rather than silently emitting an event no Kind names.
func kindOf(p Payload) (Kind, error) {
	switch p.(type) {
	case RunStarted:
		return KindRunStarted, nil
	case RunFinished:
		return KindRunFinished, nil
	case RunFailed:
		return KindRunFailed, nil
	case RunInterrupted:
		return KindRunInterrupted, nil
	case LevelOpened:
		return KindLevelOpened, nil
	case LevelClosed:
		return KindLevelClosed, nil
	case NodeStarted:
		return KindNodeStarted, nil
	case NodeProduced:
		return KindNodeProduced, nil
	case NodeFailed:
		return KindNodeFailed, nil
	case NudgeApplied:
		return KindNudgeApplied, nil
	case FreezeApplied:
		return KindFreezeApplied, nil
	default:
		return "", fmt.Errorf("unhandled payload type %T", p)
	}
}

// decodePayload is kindOf's inverse: given a Kind read off the wire, it picks the concrete
// type to decode payload into. An unknown Kind is refused rather than skipped — the same
// fail-loud rule CONVENTIONS.md applies to schema versioning applies here, because silently
// dropping an event this build does not recognize would make replay wrong in a way nothing
// downstream could detect.
func decodePayload(kind Kind, payload json.RawMessage) (Payload, error) {
	switch kind {
	case KindRunStarted:
		var p RunStarted
		return p, unmarshalPayload(kind, payload, &p)
	case KindRunFinished:
		var p RunFinished
		return p, unmarshalPayload(kind, payload, &p)
	case KindRunFailed:
		var p RunFailed
		return p, unmarshalPayload(kind, payload, &p)
	case KindRunInterrupted:
		var p RunInterrupted
		return p, unmarshalPayload(kind, payload, &p)
	case KindLevelOpened:
		var p LevelOpened
		return p, unmarshalPayload(kind, payload, &p)
	case KindLevelClosed:
		var p LevelClosed
		return p, unmarshalPayload(kind, payload, &p)
	case KindNodeStarted:
		var p NodeStarted
		return p, unmarshalPayload(kind, payload, &p)
	case KindNodeProduced:
		var p NodeProduced
		return p, unmarshalPayload(kind, payload, &p)
	case KindNodeFailed:
		var p NodeFailed
		return p, unmarshalPayload(kind, payload, &p)
	case KindNudgeApplied:
		var p NudgeApplied
		return p, unmarshalPayload(kind, payload, &p)
	case KindFreezeApplied:
		var p FreezeApplied
		return p, unmarshalPayload(kind, payload, &p)
	default:
		return nil, fmt.Errorf("unknown event kind %q", kind)
	}
}

func unmarshalPayload[T any](kind Kind, raw json.RawMessage, out *T) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("payload for kind %q: %w", kind, err)
	}
	return nil
}
