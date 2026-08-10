---
id: 0035
feature: courtage-ner-person
branch: feature/courtage-ner-person
status: done
files:
  - internal/cmd/courtage_anon.go
  - internal/cmd/courtage_ner_onnx.go
  - internal/cmd/courtage_ner_noop.go
tests:
  - internal/cmd/courtage_anon_test.go
  - internal/cmd/courtage_anon_onnx_test.go
decisions:
  - "2026-08-07 : kern-anon avait déjà un moteur ONNX BERT-NER complet (analyzer, tokenizer, recognizer PERSON→PERSON) derrière un build tag `onnx`, jamais testé ni câblé — ce n'était pas 'bloqué', juste non terminé. Fait le tour avant de conclure que c'était externe."
  - "2026-08-07 : modèle Xenova/bert-base-multilingual-cased-ner-hrl (multilingue) plutôt que le Xenova/bert-base-NER (anglais seul) déjà documenté dans kern-anon — les documents de courtage sont en français. Même architecture BERT/WordPiece, donc compatible sans changement de tokenizer, voir Kern-Anon/docs/index/... pour le détail de cette découverte."
  - "2026-08-07 : LOCATION/ORGANIZATION détectés par NER mais explicitement exclus du masquage (filterNerScope) — une ville ou le nom d'une banque partenaire est du contexte métier utile à l'interprétation, pas du PII client. Seul PERSON est masqué, conformément au besoin exact ('détection de noms propres')."
  - "2026-08-07 : singleton chargé une fois par processus (sync.Once), pas par appel — un modèle ONNX de 170 Mo rechargé à chaque masquage (deux passes par run courtage-extraction) aurait rendu le masquage inutilisable en pratique."
  - "2026-08-07 : build tag `onnx` + variable d'environnement KERN_ANON_NER_MODEL_DIR, aucune des deux seule ne suffit — le binaire par défaut (go build ./..., sans CGO) reste inchangé, cohérent avec le patron 'vide = repli sûr' déjà établi (Mistral OCR, Telegram, X)."
---

**Quoi** : détection et masquage des noms de personnes dans `courtage-extraction`, via le
moteur ONNX BERT-NER déjà présent (mais jamais câblé ni testé) dans `kern-anon`. Un texte
masqué contient maintenant `<PERSONNE_N>` pour un nom détecté, en plus des entités à motif
fixe déjà masquées (IBAN, email, téléphone...). Organisations et lieux détectés par le même
moteur restent en clair — décision délibérée, pas un oubli.

**Vérifié en réel** : `go build ./...`/`go test ./...` (build par défaut, sans le tag
`onnx`) inchangés et verts — aucune régression sur le chemin qui tourne en production
aujourd'hui. `CGO_ENABLED=1 go build -tags onnx ./...` compile. Un vrai test d'intégration
(modèle ONNX réellement chargé, aucun mock) confirme le masquage d'un nom réel et la
conservation d'un nom de ville. **Bout en bout réel** : un vrai dispatch HTTP à travers le
pipeline complet `courtage-extraction` (binaire compilé `-tags onnx`, vrai document PDF
contenant "Jean Dupont") a produit `masked_text` avec `<PERSONNE_1>` à la place du nom,
avant que quoi que ce soit ne soit envoyé à Claude.

**Pièges** :
- Version onnxruntime : 1.26.0 (documentée dans le script Linux existant) répond une
  version d'API trop ancienne pour `github.com/yalue/onnxruntime_go` v1.31.0 sur macOS —
  il faut 1.28.0. Voir `Kern-Anon/scripts/download-model-macos.sh`.
- Le modèle NER anglais déjà documenté dans `kern-anon` (`Xenova/bert-base-NER`) ne
  convient pas à des documents français — a fallu identifier et vérifier un modèle
  multilingue compatible avec le même tokenizer (BERT/WordPiece) plutôt que d'utiliser le
  modèle par défaut sans vérifier sa couverture linguistique.
