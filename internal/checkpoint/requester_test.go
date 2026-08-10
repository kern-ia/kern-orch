package checkpoint

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestRequesterRoundTrips(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "req.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	err = st.Save(ctx, Record{
		RunID: "r1", Step: 0, Frontier: []string{"a"},
		State: graph.NewState(), Status: StatusRunning,
		Requester: "yoann",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, ok, err := st.Latest(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if rec.Requester != "yoann" {
		t.Fatalf("Requester = %q; want yoann", rec.Requester)
	}
}

// Empty means open — most runs today (CLI-started) carry no requester at all.
func TestRequesterDefaultsToEmpty(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "req2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	err = st.Save(ctx, Record{
		RunID: "r1", Step: 0, Frontier: []string{"a"},
		State: graph.NewState(), Status: StatusRunning,
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, ok, err := st.Latest(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if rec.Requester != "" {
		t.Fatalf("Requester = %q; want empty", rec.Requester)
	}
}
