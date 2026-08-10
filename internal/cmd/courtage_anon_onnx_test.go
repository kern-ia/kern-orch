//go:build onnx

package cmd

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yoann/kern-orch/internal/graph"
)

// Real integration test against the ONNX NER engine — skipped unless
// KERN_ANON_NER_MODEL_DIR points at a real downloaded model (see
// Kern-Anon/scripts/download-model-macos.sh). Not mocked: the value here is proving the
// real model detects a real French name, not our own glue code.
func requireNerModel(t *testing.T) {
	t.Helper()
	if os.Getenv("KERN_ANON_NER_MODEL_DIR") == "" {
		t.Skip("KERN_ANON_NER_MODEL_DIR not set, skipping real NER integration test")
	}
}

func TestAnonymizePIIMasksAPersonNameWhenNerIsConfigured(t *testing.T) {
	requireNerModel(t)

	s := graph.NewState()
	s.Set("extracted_text", "Le client, Jean Dupont, domicilié à Vannes, a fourni son IBAN FR7630006000011234567890189.")

	if err := anonymizePII(context.Background(), s); err != nil {
		t.Fatalf("anonymizePII: %v", err)
	}

	masked, _ := s.Get("masked_text")
	maskedText := masked.(string)
	if strings.Contains(maskedText, "Jean Dupont") {
		t.Errorf("masked_text still contains the raw name: %q", maskedText)
	}
	if !strings.Contains(maskedText, "<PERSONNE_1>") {
		t.Errorf("masked_text does not contain a PERSONNE token: %q", maskedText)
	}

	// Vannes (LOCATION) is deliberately NOT masked — see filterNerScope's doc.
	if !strings.Contains(maskedText, "Vannes") {
		t.Errorf("masked_text should keep the town name (business context), got %q", maskedText)
	}
}
