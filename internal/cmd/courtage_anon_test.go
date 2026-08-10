package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/YoLaub/PresidioGo/pii"
	"github.com/yoann/kern-orch/internal/graph"
)

func TestAnonymizePIIMasksIbanAndEmailButKeepsOrdinaryText(t *testing.T) {
	s := graph.NewState()
	s.Set("extracted_text", "Salaire net 2400 euros. IBAN FR7630006000011234567890189. Contact: jean.dupont@example.com.")

	if err := anonymizePII(context.Background(), s); err != nil {
		t.Fatalf("anonymizePII: %v", err)
	}

	masked, ok := s.Get("masked_text")
	if !ok {
		t.Fatal("masked_text not set")
	}
	maskedText := masked.(string)

	if strings.Contains(maskedText, "FR7630006000011234567890189") {
		t.Errorf("masked_text still contains the raw IBAN: %q", maskedText)
	}
	if strings.Contains(maskedText, "jean.dupont@example.com") {
		t.Errorf("masked_text still contains the raw email: %q", maskedText)
	}
	if !strings.Contains(maskedText, "Salaire net 2400 euros") {
		t.Errorf("masked_text lost ordinary text: %q", maskedText)
	}

	tm, ok := s.Get("pii_token_map")
	if !ok {
		t.Fatal("pii_token_map not set")
	}
	tokenMap := tm.(map[string]string)
	if len(tokenMap) != 2 {
		t.Fatalf("expected 2 masked entities, got %d: %#v", len(tokenMap), tokenMap)
	}
	var sawIban, sawEmail bool
	for token, original := range tokenMap {
		if original == "FR7630006000011234567890189" {
			sawIban = true
			if !strings.Contains(maskedText, token) {
				t.Errorf("masked_text does not contain the IBAN's own token %q", token)
			}
		}
		if original == "jean.dupont@example.com" {
			sawEmail = true
		}
	}
	if !sawIban || !sawEmail {
		t.Errorf("token map missing an entity: %#v", tokenMap)
	}
}

func TestAnonymizePIIErrorsWhenExtractedTextMissing(t *testing.T) {
	s := graph.NewState()

	if err := anonymizePII(context.Background(), s); err == nil {
		t.Fatal("expected an error when extracted_text is absent")
	}
}

func TestAnonymizePIIIsIdempotentOnTextWithNoPII(t *testing.T) {
	s := graph.NewState()
	s.Set("extracted_text", "Bonjour, ceci est un texte sans donnée sensible.")

	if err := anonymizePII(context.Background(), s); err != nil {
		t.Fatalf("anonymizePII: %v", err)
	}

	masked, _ := s.Get("masked_text")
	if masked.(string) != "Bonjour, ceci est un texte sans donnée sensible." {
		t.Errorf("text with no PII should pass through unchanged, got %q", masked)
	}
	tm, _ := s.Get("pii_token_map")
	if len(tm.(map[string]string)) != 0 {
		t.Errorf("expected an empty token map, got %#v", tm)
	}
}

func TestDeanonymizePIIRestoresOriginalValues(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation_masked", `{"iban":"<IBAN_1>","email":"<EMAIL_1>"}`)
	s.Set("pii_token_map", map[string]string{
		"<IBAN_1>":  "FR7630006000011234567890189",
		"<EMAIL_1>": "jean.dupont@example.com",
	})

	if err := deanonymizePII(context.Background(), s); err != nil {
		t.Fatalf("deanonymizePII: %v", err)
	}

	result, ok := s.Get("interpretation")
	if !ok {
		t.Fatal("interpretation not set")
	}
	want := `{"iban":"FR7630006000011234567890189","email":"jean.dupont@example.com"}`
	if result.(string) != want {
		t.Errorf("got %q, want %q", result.(string), want)
	}
}

// A checkpoint round-trip serializes state through JSON, so a map[string]string set
// before a Freeze/persist comes back as map[string]interface{} after reload — this must
// not break restoration.
func TestDeanonymizePIIHandlesTokenMapAfterJSONRoundTrip(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation_masked", `Le titulaire est <PII_1>.`)
	s.Set("pii_token_map", map[string]interface{}{
		"<PII_1>": "Jean Dupont",
	})

	if err := deanonymizePII(context.Background(), s); err != nil {
		t.Fatalf("deanonymizePII: %v", err)
	}

	result, _ := s.Get("interpretation")
	if result.(string) != "Le titulaire est Jean Dupont." {
		t.Errorf("got %q", result.(string))
	}
}

// anonymizeMemoInput/deanonymizeMemoOutput are the same masking logic under distinct
// state keys (memo_text/memo_masked_text/memo_token_map, memo_draft_masked/memo_draft) —
// besoin #2 (mémorandum) reuses the extraction step's sovereignty discipline without
// clobbering extraction's own extracted_text/masked_text/interpretation keys, since both
// phases now run inside the same graph/state (see examples/courtage-extraction.yaml).
func TestAnonymizeMemoInputUsesItsOwnStateKeys(t *testing.T) {
	s := graph.NewState()
	s.Set("extracted_text", "ne doit pas être touché")
	s.Set("memo_text", "Contact client : jean.dupont@example.com")

	if err := anonymizeMemoInput(context.Background(), s); err != nil {
		t.Fatalf("anonymizeMemoInput: %v", err)
	}

	extracted, _ := s.Get("extracted_text")
	if extracted.(string) != "ne doit pas être touché" {
		t.Errorf("anonymizeMemoInput must not touch extracted_text, got %q", extracted)
	}
	masked, ok := s.Get("memo_masked_text")
	if !ok || strings.Contains(masked.(string), "jean.dupont@example.com") {
		t.Fatalf("memo_masked_text not set or still contains the raw email: %v", masked)
	}
	if _, ok := s.Get("masked_text"); ok {
		t.Error("anonymizeMemoInput must not write the extraction step's masked_text key")
	}
}

func TestDeanonymizeMemoOutputUsesItsOwnStateKeys(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation", "ne doit pas être touché")
	s.Set("memo_draft_masked", "Contact : <EMAIL_1>")
	s.Set("memo_token_map", map[string]string{"<EMAIL_1>": "jean.dupont@example.com"})

	if err := deanonymizeMemoOutput(context.Background(), s); err != nil {
		t.Fatalf("deanonymizeMemoOutput: %v", err)
	}

	draft, ok := s.Get("memo_draft")
	if !ok || draft.(string) != "Contact : jean.dupont@example.com" {
		t.Fatalf("memo_draft not restored correctly: %v", draft)
	}
	interp, _ := s.Get("interpretation")
	if interp.(string) != "ne doit pas être touché" {
		t.Errorf("deanonymizeMemoOutput must not touch interpretation, got %q", interp)
	}
}

// Kern-UI's hive panel shows a node's output when state carries "display:<nodeId>" (any
// node can opt in — see Kern-UI/web/src/runs/HiveGraph.tsx, outputOf). demasquage_memo is
// the graph node id that runs deanonymizeMemoOutput (examples/courtage-extraction.yaml) —
// writing "display:demasquage_memo" here is what makes the final, demasked memorandum
// actually visible in the UI instead of only reachable through the raw run state.
func TestDeanonymizeMemoOutputWritesTheHiveDisplayKey(t *testing.T) {
	s := graph.NewState()
	s.Set("memo_draft_masked", "Contenu du mémorandum.")
	s.Set("memo_token_map", map[string]string{})

	if err := deanonymizeMemoOutput(context.Background(), s); err != nil {
		t.Fatalf("deanonymizeMemoOutput: %v", err)
	}

	display, ok := s.Get("display:demasquage_memo")
	if !ok || display.(string) != "Contenu du mémorandum." {
		t.Fatalf("display:demasquage_memo not set correctly: %v", display)
	}
}

// deanonymizePII (besoin #1) deliberately gets NO display key here — the dossier it
// restores is meant to be reviewed at confirm_extraction via its own approval context,
// not surfaced through this generic hive convention; only besoin #2's memo was asked for.
func TestDeanonymizePIIDoesNotWriteADisplayKey(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation_masked", "{}")
	s.Set("pii_token_map", map[string]string{})

	if err := deanonymizePII(context.Background(), s); err != nil {
		t.Fatalf("deanonymizePII: %v", err)
	}

	if _, ok := s.Get("display:demasquage_pii"); ok {
		t.Error("deanonymizePII must not write a display key (not requested)")
	}
}

// filterNerScope keeps only PERSON among NER-derived entity types — LOCATION/ORGANIZATION
// are legitimate business context (a bank's name, a town) that the interpreting model
// still needs to see; only a person's identity is what "détection de noms propres" asked
// to mask. Regex-based entity types (IBAN_CODE, EMAIL_ADDRESS, ...) are untouched either
// way — this filter only ever removes LOCATION/ORGANIZATION.
func TestFilterNerScopeKeepsPersonAndRegexEntitiesButDropsLocationAndOrganization(t *testing.T) {
	in := []pii.Result{
		{EntityType: "PERSON", Start: 0, End: 4},
		{EntityType: "LOCATION", Start: 5, End: 9},
		{EntityType: "ORGANIZATION", Start: 10, End: 14},
		{EntityType: "IBAN_CODE", Start: 15, End: 20},
	}

	got := filterNerScope(in)

	var types []string
	for _, r := range got {
		types = append(types, r.EntityType)
	}
	if len(types) != 2 || types[0] != "PERSON" || types[1] != "IBAN_CODE" {
		t.Errorf("got %v, want [PERSON IBAN_CODE]", types)
	}
}

func TestFilterNerScopeHandlesAnEmptyInput(t *testing.T) {
	got := filterNerScope(nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestDeanonymizePIIIsSafeWithoutATokenMap(t *testing.T) {
	s := graph.NewState()
	s.Set("interpretation_masked", "Rien à démasquer ici.")

	if err := deanonymizePII(context.Background(), s); err != nil {
		t.Fatalf("deanonymizePII: %v", err)
	}

	result, _ := s.Get("interpretation")
	if result.(string) != "Rien à démasquer ici." {
		t.Errorf("got %q", result.(string))
	}
}
