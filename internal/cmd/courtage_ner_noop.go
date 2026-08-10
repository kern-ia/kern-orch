//go:build !onnx

package cmd

import "github.com/YoLaub/PresidioGo/nlp"

// nlpEngine is nil in the default build (no -tags onnx) — kern-anon's regex recognizers
// (IBAN, email, phone, ...) still run; only NER-based person-name detection is absent.
// See courtage_ner_onnx.go for the real implementation.
func nlpEngine() nlp.Engine { return nil }
