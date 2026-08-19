package cmd

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/yoann/kern-orch/internal/agentrunner"
	"github.com/yoann/kern-orch/internal/checkpoint"
	"github.com/yoann/kern-orch/internal/config"
	"github.com/yoann/kern-orch/internal/graph"
	"github.com/yoann/kern-orch/internal/journal/projection"
	"github.com/yoann/kern-orch/internal/report"
	"github.com/yoann/kern-orch/internal/topology"
)

// buildChildRunHooks is nestedRuns' own factory (see runtime.go), reachable directly rather
// than only through a built graph — the run id it mints is otherwise invisible until an
// event lands under it.

// Two calls must mint two different run ids: a subgraph node retried or looped is two
// nested runs, never one run journalled twice (see graph.SubgraphNode's childRun doc on why
// reporting and journalling have to agree on the id — this asserts the journalling half).
func TestBuildChildRunHooksMintsAFreshRunIDPerCall(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cp.db")
	cfg := config.Config{CheckpointDB: dbPath}
	build := buildChildRunHooks(report.NewHTTP(""), cfg, "parent-run")

	first := build("nested", "child.yaml")
	second := build("nested", "child.yaml")
	if first == nil || first.Event == nil {
		t.Fatalf("first call returned %+v, want an Event hook", first)
	}
	if second == nil || second.Event == nil {
		t.Fatalf("second call returned %+v, want an Event hook", second)
	}
	defer first.Close()
	defer second.Close()

	if err := first.Event(context.Background(), graph.Event{Kind: graph.EventRunStarted}); err != nil {
		t.Fatalf("first.Event: %v", err)
	}
	if err := first.Event(context.Background(), graph.Event{Kind: graph.EventRunFinished}); err != nil {
		t.Fatalf("first.Event: %v", err)
	}
	if err := second.Event(context.Background(), graph.Event{Kind: graph.EventRunStarted}); err != nil {
		t.Fatalf("second.Event: %v", err)
	}
	if err := second.Event(context.Background(), graph.Event{Kind: graph.EventRunFinished}); err != nil {
		t.Fatalf("second.Event: %v", err)
	}

	runIDs := distinctRunIDs(t, dbPath)
	if len(runIDs) != 2 {
		t.Fatalf("journal holds events under %d distinct run ids, want 2: %v", len(runIDs), runIDs)
	}
}

// The journal is this codebase's source of truth: a nested run gets one whether or not
// anyone is watching over HTTP. A disabled reporter must not also disable the journal.
func TestBuildChildRunHooksJournalsWithNoReporterConfigured(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cp.db")
	cfg := config.Config{CheckpointDB: dbPath}
	build := buildChildRunHooks(report.NewHTTP(""), cfg, "parent-run")

	hooks := build("nested", "child.yaml")
	if hooks == nil {
		t.Fatal("buildChildRunHooks returned nil with the reporter disabled, want a journalling-only hook set")
	}
	if hooks.Step != nil {
		t.Error("Step is set although the reporter is disabled")
	}
	if hooks.Event == nil {
		t.Fatal("Event is nil although a checkpoint store is configured")
	}
	defer hooks.Close()

	if err := hooks.Event(context.Background(), graph.Event{Kind: graph.EventRunStarted}); err != nil {
		t.Fatalf("Event: %v", err)
	}
	if err := hooks.Event(context.Background(), graph.Event{Kind: graph.EventRunFinished}); err != nil {
		t.Fatalf("Event: %v", err)
	}
	runIDs := distinctRunIDs(t, dbPath)
	if len(runIDs) != 1 {
		t.Fatalf("journal holds events under %d distinct run ids, want 1: %v", len(runIDs), runIDs)
	}
}

// A store that cannot be opened must never stop the nested run itself — it costs that one
// child its own journal, not the run (see buildChildRunHooks' comment on why).
func TestBuildChildRunHooksDegradesWhenTheStoreCannotOpen(t *testing.T) {
	// openStore's parent-directory creation (os.MkdirAll) fails when a path component it
	// needs to walk through already exists as a plain file rather than a directory.
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write %q: %v", blocked, err)
	}
	cfg := config.Config{CheckpointDB: filepath.Join(blocked, "sub", "cp.db")}
	build := buildChildRunHooks(report.NewHTTP(""), cfg, "parent-run")

	hooks := build("nested", "child.yaml")
	if hooks != nil {
		t.Fatalf("buildChildRunHooks = %+v, want nil when the store cannot be opened and reporting is off", hooks)
	}
}

// distinctRunIDs opens its own connection to the checkpoint database and reads back every
// run id the events table holds — the store type has no such query itself (it always
// already knows which run it wants), so a test proving *how many* runs exist has to look
// underneath it.
func distinctRunIDs(t *testing.T, dbPath string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open %q: %v", dbPath, err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT DISTINCT run_id FROM events`)
	if err != nil {
		t.Fatalf("query distinct run ids: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan run id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate run ids: %v", err)
	}
	return ids
}

// The acceptance case named in the issue: running examples/parent.yaml produces two
// journals — the parent's, and the "nested" node's own child — each independently
// monotonic and each replayable to its final state, including what the child merged back
// into the parent.
func TestParentExampleGraphWritesAParentJournalAndAChildJournal(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cp.db")
	cfg := config.Config{CheckpointDB: dbPath}
	st, err := checkpoint.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	reg := builtinRegistry(&agentrunner.Stub{}, cfg)
	const parentRunID = "parent-run"
	nestedRuns(reg, report.NewHTTP(""), cfg, parentRunID)

	g, err := topology.LoadFile("../../examples/parent.yaml", reg)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	parentRec, err := newJournalRecorder(context.Background(), st, parentRunID, "parent")
	if err != nil {
		t.Fatalf("newJournalRecorder: %v", err)
	}
	engine := graph.NewEngine(g).OnEvent(multiEvent(parentRec.record))
	if err := engine.Run(context.Background(), graph.NewState()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	runIDs := distinctRunIDs(t, dbPath)
	if len(runIDs) != 2 {
		t.Fatalf("database holds events under %d run ids, want 2 (parent + child): %v", len(runIDs), runIDs)
	}
	var childRunID string
	for _, id := range runIDs {
		if id != parentRunID {
			childRunID = id
		}
	}
	if childRunID == "" {
		t.Fatalf("no run id besides the parent's %q was found among %v", parentRunID, runIDs)
	}

	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		runID string
	}{
		{"parent", parentRunID},
		{"child", childRunID},
	} {
		events, err := st.Read(ctx, tc.runID)
		if err != nil {
			t.Fatalf("%s: Read: %v", tc.name, err)
		}
		for i, ev := range events {
			want := int64(checkpoint.FirstSeq) + int64(i)
			if ev.Seq != want {
				t.Fatalf("%s: event %d has seq %d, want %d — the sequence is not independently monotonic", tc.name, i, ev.Seq, want)
			}
		}
		state, err := projection.Project(events)
		if err != nil {
			t.Fatalf("%s: Project: %v", tc.name, err)
		}
		// The journal round-trips through JSON (see journal.Payload), so a number replayed
		// from it comes back as float64 even though the tool that wrote it set an int.
		n, ok := state.Get("n")
		if !ok || n != float64(6) {
			t.Fatalf("%s: replayed state n = %v (%T) (ok %t), want 6 — seed(3) doubled by the child", tc.name, n, n, ok)
		}
	}
}
