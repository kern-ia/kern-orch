package checkpoint

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

func TestDossierRoundTrips(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "dos.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	err = st.Save(ctx, Record{
		RunID: "r1", Step: 0, Frontier: []string{"a"},
		State: graph.NewState(), Status: StatusRunning,
		Dossier: "AF-2288",
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	rec, ok, err := st.Latest(ctx, "r1")
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if rec.Dossier != "AF-2288" {
		t.Fatalf("Dossier = %q; want AF-2288", rec.Dossier)
	}
}

// Empty means no dossier — most runs today (CLI-started, or dispatched free-form) carry
// none at all.
func TestDossierDefaultsToEmpty(t *testing.T) {
	st, err := OpenSQLite(filepath.Join(t.TempDir(), "dos2.db"))
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
	if rec.Dossier != "" {
		t.Fatalf("Dossier = %q; want empty", rec.Dossier)
	}
}
