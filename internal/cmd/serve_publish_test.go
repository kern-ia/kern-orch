package cmd

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yoann/kern-orch/internal/config"
)

// TestServePublishesTheCatalogueAtStartup guards against the gap found preparing the
// 2026-08 demo: `kern-orch serve` ran for hours pushing runs to kern-ui, and kern-ui's
// Grimoire stayed on "catalogue not received" the entire time — only the one-shot
// `publish-skills` command and `run` ever called publishRegistry. A long-lived daemon must
// not need a human to run a second command just to be visible in the interface it feeds.
func TestServePublishesTheCatalogueAtStartup(t *testing.T) {
	sink := newCatalogueSink(t)
	dir := t.TempDir()

	t.Setenv(config.EnvRegistryReportURL, sink.server.URL)
	t.Setenv(config.EnvSkillsDir, skillsDir(t))
	t.Setenv(config.EnvCheckpointDB, filepath.Join(dir, "k.db"))
	t.Setenv(config.EnvServeAddr, "127.0.0.1:0")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	root := newRootCmd()
	root.SetArgs([]string{"serve"})
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()

	deadline := time.After(2 * time.Second)
	for sink.count() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatal("serve never published the catalogue")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down after its context was cancelled")
	}

	body := sink.last()
	if body["source"] != "kern-orch" {
		t.Errorf("source = %v, want kern-orch", body["source"])
	}
}
