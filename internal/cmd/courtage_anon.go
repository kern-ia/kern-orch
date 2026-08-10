package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/YoLaub/PresidioGo/analyzer"
	"github.com/YoLaub/PresidioGo/anonymizer"
	"github.com/YoLaub/PresidioGo/pii"
	"github.com/YoLaub/PresidioGo/recognizers/ner"
	"github.com/YoLaub/PresidioGo/registry"
	"github.com/yoann/kern-orch/internal/graph"
)

// piiTokenLabels maps kern-anon's fr/generic recognizer entity types to the short,
// human-readable prefix used in the sequential mask token (<PREFIX_N>). Any entity type
// not listed here (a future recognizer this file wasn't updated for) still gets masked
// through the DEFAULT operator below with prefix "PII" — never left in clear, only less
// self-descriptive to the interpreting model.
var piiTokenLabels = map[string]string{
	"FR_NIR":           "NIR",
	"FR_SIREN":         "SIREN",
	"FR_SIRET":         "SIRET",
	"FR_LICENSE_PLATE": "PLAQUE",
	"FR_PHONE_NUMBER":  "TELEPHONE",
	"EMAIL_ADDRESS":    "EMAIL",
	"IBAN_CODE":        "IBAN",
	"CREDIT_CARD":      "CARTE",
	"CRYPTO":           "CRYPTO",
	"MAC_ADDRESS":      "MAC",
	"IP_ADDRESS":       "IP",
	"URL":              "URL",
	// PERSON only appears in results when nlpEngine() is non-nil (see
	// courtage_ner_onnx.go/courtage_ner_noop.go) — kern-anon's regex recognizers alone
	// cannot detect a name in free text, only structured patterns.
	"PERSON": "PERSONNE",
}

// nerEntitiesToDrop are results the NER recognizer can produce that filterNerScope
// excludes — LOCATION and ORGANIZATION are legitimate business context (a bank's name, a
// client's town) the interpreting model still needs; "détection de noms propres" asked
// for person identity, not every entity a general-purpose NER model happens to also tag.
var nerEntitiesToDrop = map[string]bool{"LOCATION": true, "ORGANIZATION": true}

// filterNerScope drops nerEntitiesToDrop from results. Regex-based entity types
// (IBAN_CODE, EMAIL_ADDRESS, ...) are never affected — kern-anon's registry never emits
// LOCATION/ORGANIZATION from a regex recognizer, only from NER.
func filterNerScope(results []pii.Result) []pii.Result {
	kept := make([]pii.Result, 0, len(results))
	for _, r := range results {
		if nerEntitiesToDrop[r.EntityType] {
			continue
		}
		kept = append(kept, r)
	}
	return kept
}

// newMaskOperators builds one Custom operator per known entity type (plus a DEFAULT
// catch-all), each writing its masked value into a shared token map instead of Presidio's
// own position-based Deanonymize. A Claude-transformed JSON output cannot preserve the
// original text's rune offsets, so restoring PII by exact position (Deanonymize's
// contract) does not fit this pipeline — plain string substitution against this map does,
// regardless of how the text in between was rewritten.
func newMaskOperators() (map[string]anonymizer.Operator, map[string]string) {
	tokenMap := make(map[string]string)
	counters := make(map[string]int)

	mk := func(label string) anonymizer.Operator {
		return anonymizer.Custom("mask_token", func(v string) (string, error) {
			counters[label]++
			token := fmt.Sprintf("<%s_%d>", label, counters[label])
			tokenMap[token] = v
			return token, nil
		})
	}

	ops := make(map[string]anonymizer.Operator, len(piiTokenLabels)+1)
	for entityType, label := range piiTokenLabels {
		ops[entityType] = mk(label)
	}
	ops["DEFAULT"] = mk("PII")
	return ops, tokenMap
}

// newAnonymizeTool builds a tool that masks PII in state key inputKey and writes
// textOutKey/mapOutKey (token -> original value). Parameterized by key names so more than
// one masking pass can run inside the same graph/state without clobbering another pass's
// keys — besoin #1 (extraction) and besoin #2 (mémorandum, courtage-extraction.yaml) both
// mask something before a model sees it, and now run inside the SAME state (the user
// chose a chained single flow for besoin #2), so they need distinct key names. Errors
// rather than proceeding on an absent input: a missing inputKey means an upstream node
// didn't run, not that there is nothing to mask.
func newAnonymizeTool(inputKey, textOutKey, mapOutKey string) func(context.Context, *graph.State) error {
	return func(ctx context.Context, s *graph.State) error {
		raw, ok := s.Get(inputKey)
		if !ok {
			return fmt.Errorf("courtage: %s manquant", inputKey)
		}
		text, _ := raw.(string)

		reg := registry.Default("fr")
		opts := []analyzer.Option{analyzer.WithRegistry(reg), analyzer.WithDefaultLanguage("fr")}
		// nlpEngine() is nil without -tags onnx or KERN_ANON_NER_MODEL_DIR (see
		// courtage_ner_onnx.go/courtage_ner_noop.go) — the regex recognizers above still
		// run either way, this only adds PERSON detection on top when available.
		if eng := nlpEngine(); eng != nil {
			reg.Add(ner.New("fr"))
			opts = append(opts, analyzer.WithNlpEngine(eng))
		}
		analyzerEng, err := analyzer.New(opts...)
		if err != nil {
			return fmt.Errorf("courtage: initialisation de l'analyseur PII : %w", err)
		}
		results, err := analyzerEng.Analyze(ctx, text, analyzer.Language("fr"))
		if err != nil {
			return fmt.Errorf("courtage: analyse PII : %w", err)
		}
		results = filterNerScope(results)

		ops, tokenMap := newMaskOperators()
		result, err := anonymizer.New().Anonymize(text, results, ops)
		if err != nil {
			return fmt.Errorf("courtage: masquage PII : %w", err)
		}

		s.Set(textOutKey, result.Text)
		s.Set(mapOutKey, tokenMap)
		return nil
	}
}

// newDeanonymizeTool builds a tool that restores original PII values into state key
// inputKey (a model's masked-text output) by plain string substitution against mapKey,
// writing the result to outKey. A checkpoint round-trip serializes State through JSON, so
// a map[string]string set by the matching anonymize tool before a Freeze/persist comes
// back as map[string]interface{} on reload — both shapes are accepted (asStringMap). A
// missing or empty token map is not an error: it means the input text had no PII to mask
// in the first place.
//
// displayKey, when non-empty, also writes the result under that key — the convention
// Kern-UI's hive panel reads to show a node's output (any node can opt in by writing
// "display:<nodeId>", see Kern-UI/web/src/runs/HiveGraph.tsx). A Go tool node has no
// nodeID at runtime (ToolFunc's signature carries none), unlike a Python agent node's own
// handler; the node id each of these tools runs at is fixed by construction (one graph,
// one call site each), so baking it in here is accurate, not a guess.
func newDeanonymizeTool(inputKey, mapKey, outKey, displayKey string) func(context.Context, *graph.State) error {
	return func(_ context.Context, s *graph.State) error {
		raw, _ := s.Get(inputKey)
		text, _ := raw.(string)

		tm, _ := s.Get(mapKey)
		for token, original := range asStringMap(tm) {
			text = strings.ReplaceAll(text, token, original)
		}

		s.Set(outKey, text)
		if displayKey != "" {
			s.Set(displayKey, text)
		}
		return nil
	}
}

// anonymizePII/deanonymizePII: besoin #1 (extraction) — see specs.md "Besoin #1". No
// display key: the restored dossier is meant to be reviewed at confirm_extraction through
// its own approval context, not surfaced through the generic hive convention.
var anonymizePII = newAnonymizeTool("extracted_text", "masked_text", "pii_token_map")
var deanonymizePII = newDeanonymizeTool("interpretation_masked", "pii_token_map", "interpretation", "")

// anonymizeMemoInput/deanonymizeMemoOutput: besoin #2 (mémorandum) — own key names so
// this second masking pass never touches besoin #1's extracted_text/masked_text/
// interpretation, which must survive untouched in the final state (the analyst needs both
// the structured dossier AND the memo draft, not one overwriting the other).
// deanonymizeMemoOutput runs at graph node "demasquage_memo" (examples/courtage-extraction.yaml)
// — writing display:demasquage_memo is what makes the final memo actually visible in
// Kern-UI instead of only reachable through the raw run state.
var anonymizeMemoInput = newAnonymizeTool("memo_text", "memo_masked_text", "memo_token_map")
var deanonymizeMemoOutput = newDeanonymizeTool("memo_draft_masked", "memo_token_map", "memo_draft", "display:demasquage_memo")

func asStringMap(v any) map[string]string {
	switch m := v.(type) {
	case map[string]string:
		return m
	case map[string]any:
		out := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}
