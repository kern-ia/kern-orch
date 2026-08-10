//go:build onnx

package cmd

import (
	"fmt"
	"os"
	"sync"

	"github.com/YoLaub/PresidioGo/nlp"
	"github.com/YoLaub/PresidioGo/nlp/onnx"
)

var (
	nlpEngineOnce sync.Once
	nlpEngineVal  nlp.Engine
)

// nlpEngine returns the shared ONNX NER engine, loaded once and reused across every
// masking call in the process — the model is ~170MB and Load() builds an onnxruntime
// session; reloading it per call (courtage-extraction masks twice: extraction, then
// mémorandum) would make masking prohibitively slow.
//
// Same "vide = repli sûr" pattern as Mistral OCR / Telegram / X: unset
// KERN_ANON_NER_MODEL_DIR (or a build without -tags onnx at all, see courtage_ner_noop.go)
// means no person-name detection, not a broken masking pipeline — kern-anon's regex
// recognizers (IBAN, email, phone, ...) run regardless, this only adds PERSON on top.
func nlpEngine() nlp.Engine {
	nlpEngineOnce.Do(func() {
		dir := os.Getenv("KERN_ANON_NER_MODEL_DIR")
		if dir == "" {
			return
		}
		eng := onnx.New(dir)
		if err := eng.Load(); err != nil {
			// A bad model path must not take the whole masking pipeline down — logs and
			// falls back to no NER, same anti-fragile posture as the rest of this file.
			fmt.Fprintf(os.Stderr, "courtage: échec du chargement du moteur NER (%s) : %v\n", dir, err)
			return
		}
		nlpEngineVal = eng
	})
	return nlpEngineVal
}
