// Package config resolves harness configuration from environment variables with sane
// defaults. Precedence is: explicit CLI flags (applied by the caller) over env over
// these defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Environment variable names.
const (
	EnvSkillsDir = "KERN_SKILLS_DIR"
	// EnvSkillsCustomDir is where a skill created through C11's write path is written —
	// a separate directory from EnvSkillsDir on purpose: a product update can overwrite
	// EnvSkillsDir wholesale without ever touching a creation.
	EnvSkillsCustomDir = "KERN_SKILLS_CUSTOM_DIR"
	EnvCheckpointDB    = "KERN_CHECKPOINT_DB"
	EnvAgentCLI        = "KERN_AGENT_CLI"
	// EnvAgentKind names which agent CLI EnvAgentCLI points at, selecting the adapter that
	// knows that CLI's wire protocol. It is its own variable rather than something derived
	// from EnvAgentCLI's path because the path is an operator's choice — a wrapper script, a
	// version-pinned shim, a binary renamed for a fleet — and sniffing a kind out of it would
	// make the harness guess at the one thing it cannot afford to get wrong: which protocol
	// it is about to speak. Empty is legal only while EnvAgentCLI is empty too (the stub
	// path); set one without the other and FromEnv refuses, because the alternative is a
	// silent default that talks the wrong protocol to a real binary and fails deep inside a
	// run instead of at load.
	EnvAgentKind = "KERN_AGENT_KIND"
	// EnvStepReportURL points at an HTTP sink receiving one POST per completed graph
	// level. Unset means no reporting. The URL is the whole contract: kern-orch knows
	// nothing of the sink's route shape.
	EnvStepReportURL = "KERN_STEP_REPORT_URL"
	// EnvRegistryReportURL points at an HTTP sink receiving the whole skills catalogue.
	// It is a second variable rather than a route derived from EnvStepReportURL for the
	// reason stated above: the URL is the whole contract, so kern-orch must not invent a
	// sibling path on a host it knows nothing about.
	EnvRegistryReportURL = "KERN_REGISTRY_REPORT_URL"
	// EnvActivityReportURL points at an HTTP sink receiving one signal each time an agent
	// node starts and stops working. Same reasoning as the two above: its own URL, because
	// a sibling route cannot be invented for a host we know nothing about.
	EnvActivityReportURL = "KERN_ACTIVITY_REPORT_URL"
	// EnvSinkToken is the credential presented to every sink above. One secret for the three
	// URLs: they are three contracts to the same consumer, and asking an operator to manage
	// three secrets would mostly produce three copies of one.
	EnvSinkToken = "KERN_SINK_TOKEN"

	// EnvServeAddr is where `kern-orch serve` listens.
	EnvServeAddr = "KERN_ORCH_ADDR"
	// EnvServeToken is the bearer credential a caller of the daemon API must present.
	// Empty leaves the daemon open, which `serve` refuses on a public address — the same
	// rule kern-ui enforces on its own API, re-derived here rather than shared: the two
	// bricks depend on nothing of each other's.
	EnvServeToken = "KERN_ORCH_TOKEN"

	// EnvTelegramBotToken and EnvTelegramChatID configure the `notify` builtin tool: an
	// agent node's own outbound channel to a human, distinct from the step/activity/
	// registry sinks above (those are the harness reporting on itself; this is a graph
	// choosing to speak). Either unset leaves the tool unconfigured, and a graph that
	// references it fails loud rather than dropping messages silently.
	EnvTelegramBotToken = "KERN_TELEGRAM_BOT_TOKEN"
	EnvTelegramChatID   = "KERN_TELEGRAM_CHAT_ID"

	// EnvUploadDir is where POST /api/v1/uploads saves a document — the UI-upload
	// ingestion channel, same "text IS the document path" convention as a chat command or
	// courtage-extraction's Telegram listener already use.
	EnvUploadDir = "KERN_ORCH_UPLOAD_DIR"

	// EnvRuntimeEquivalenceCheck turns on, at every level boundary, a comparison of the
	// journal's projection against the live state the engine is carrying forward (issue 10's
	// assertReplayEquivalent, wired for a live run instead of a test). It is its own variable
	// rather than folded into an existing one because it is a diagnostic for one class of bug
	// only — a divergence introduced in code (the emission sites, the projection), not one
	// carried by a run's actual data — so it must be flippable per deployment, and per run if
	// a run is under suspicion, without touching the other sinks or the checkpoint path at
	// all. Off by default: it doubles the state work of every level of every run, which is
	// too costly to pay by default to defend against a class of bug the epic's own tests
	// (issue 10) already prove does not hold in the general case.
	EnvRuntimeEquivalenceCheck = "KERN_RUNTIME_EQUIVALENCE_CHECK"
)

// Recognized values of EnvAgentKind. The list lives here rather than in agentrunner so
// FromEnv can reject an unknown kind at load without config depending on the package that
// implements the adapters — the dependency runs the other way (see CONVENTIONS.md).
const (
	AgentKindClaudeCode = "claude-code"
	AgentKindOpenCode   = "opencode"
)

// Config is the resolved runtime configuration.
type Config struct {
	SkillsDir string
	// CustomSkillsDir is where a created skill (C11) is written and read back from.
	CustomSkillsDir string
	CheckpointDB    string
	AgentCLI        string // path to external LLM CLI; empty => use the deterministic stub
	// AgentKind selects the adapter that speaks AgentCLI's protocol; empty only when
	// AgentCLI is empty too. See EnvAgentKind.
	AgentKind string
	// StepReportURL is an HTTP sink for step transitions; empty => no reporting.
	StepReportURL string
	// RegistryReportURL is an HTTP sink for the skills catalogue; empty => no publishing.
	RegistryReportURL string
	// ActivityReportURL is an HTTP sink for agent activity; empty => no reporting.
	ActivityReportURL string
	// SinkToken is presented to the sinks above; empty => reports travel anonymous.
	SinkToken string

	// ServeAddr is where `serve` listens.
	ServeAddr string
	// ServeToken is the credential the daemon API requires; empty => open (local dev only).
	ServeToken string

	// TelegramBotToken and TelegramChatID configure the `notify` builtin tool; either
	// empty leaves it unconfigured.
	TelegramBotToken string
	TelegramChatID   string

	// UploadDir is where an uploaded document is saved.
	UploadDir string

	// RuntimeEquivalenceCheck turns on the opt-in replay-equivalence check at every level
	// boundary; false (the default) means the run pays no extra projection cost at all — see
	// EnvRuntimeEquivalenceCheck for why it defaults off and is its own variable.
	RuntimeEquivalenceCheck bool
}

// FromEnv builds a Config from the environment, applying defaults for unset variables. It
// returns an error rather than falling back to a default when a variable is set to a value
// that cannot be parsed — RuntimeEquivalenceCheck is the first field this applies to — per
// CONVENTIONS.md's "misconfiguration fails loud".
func FromEnv() (Config, error) {
	equivalenceCheck, err := boolEnvOr(EnvRuntimeEquivalenceCheck, false)
	if err != nil {
		return Config{}, err
	}

	agentCLI := os.Getenv(EnvAgentCLI)
	agentKind, err := agentKindEnv(agentCLI)
	if err != nil {
		return Config{}, err
	}

	return Config{
		SkillsDir:               envOr(EnvSkillsDir, "skills"),
		CustomSkillsDir:         envOr(EnvSkillsCustomDir, "skills-custom"),
		CheckpointDB:            envOr(EnvCheckpointDB, "./data/kern-orch.db"),
		AgentCLI:                agentCLI,
		AgentKind:               agentKind,
		StepReportURL:           os.Getenv(EnvStepReportURL),
		RegistryReportURL:       os.Getenv(EnvRegistryReportURL),
		ActivityReportURL:       os.Getenv(EnvActivityReportURL),
		SinkToken:               os.Getenv(EnvSinkToken),
		ServeAddr:               envOr(EnvServeAddr, "127.0.0.1:7070"),
		ServeToken:              os.Getenv(EnvServeToken),
		TelegramBotToken:        os.Getenv(EnvTelegramBotToken),
		TelegramChatID:          os.Getenv(EnvTelegramChatID),
		UploadDir:               envOr(EnvUploadDir, "./data/uploads"),
		RuntimeEquivalenceCheck: equivalenceCheck,
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// agentKindEnv reads EnvAgentKind and checks it against agentCLI, the already-resolved value
// of EnvAgentCLI. It reports an error rather than picking a default for the same reason
// boolEnvOr does: a kind the harness guessed is a protocol mismatch discovered at the first
// agent node of a real run, when the cheap place to discover it is here.
//
// The check is keyed on agentCLI being set, not on the kind being set, so the no-CLI stub
// path keeps working untouched — that is the configuration every existing test and every
// LLM-less deployment runs under.
func agentKindEnv(agentCLI string) (string, error) {
	kind := os.Getenv(EnvAgentKind)
	if agentCLI == "" {
		return "", nil
	}
	switch kind {
	case AgentKindClaudeCode, AgentKindOpenCode:
		return kind, nil
	case "":
		return "", fmt.Errorf("config: %s is set (%q) but %s is empty: set it to %q or %q",
			EnvAgentCLI, agentCLI, EnvAgentKind, AgentKindClaudeCode, AgentKindOpenCode)
	default:
		return "", fmt.Errorf("config: %s: unknown agent kind %q: want %q or %q",
			EnvAgentKind, kind, AgentKindClaudeCode, AgentKindOpenCode)
	}
}

// boolEnvOr parses key as a bool, returning def when it is unset. An empty value is treated
// as unset (consistent with envOr above) rather than as a parse failure, but any other value
// strconv.ParseBool rejects is reported to the caller instead of silently becoming def — a
// typo in the variable's value must not be read as "check disabled".
func boolEnvOr(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("config: %s: invalid boolean value %q: %w", key, v, err)
	}
	return b, nil
}
