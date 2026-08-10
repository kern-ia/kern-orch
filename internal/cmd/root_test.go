package cmd

import "testing"

func TestRootHasExpectedSubcommands(t *testing.T) {
	want := map[string]bool{"run": true, "resume": true, "status": true, "list-skills": true, "publish-skills": true, "serve": true}
	for _, c := range newRootCmd().Commands() {
		delete(want, c.Name())
	}
	if len(want) != 0 {
		t.Fatalf("missing subcommands: %v", want)
	}
}
