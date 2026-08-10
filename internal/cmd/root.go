// Package cmd wires the Cobra command tree for the kern-orch harness.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "kern-orch",
		Short: "Agentic harness: runs an explicit graph of agents and sub-agents",
		Long: "kern-orch orchestrates a LangGraph-style graph (shared state, nodes, " +
			"conditional edges). It never calls LLMs directly — agent nodes invoke an " +
			"external multi-provider CLI as a subprocess.",
		SilenceUsage: true,
	}
	root.AddCommand(newRunCmd(), newResumeCmd(), newStatusCmd(), newListSkillsCmd(), newPublishSkillsCmd(), newServeCmd())
	return root
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
