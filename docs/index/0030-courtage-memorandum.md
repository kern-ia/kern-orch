---
id: 0030
feature: courtage-memorandum
branch: feature/courtage-extraction
status: done
files:
  - internal/cmd/courtage_anon.go
  - internal/cmd/runtime.go
  - examples/courtage-extraction.yaml
  - skills/courtage-extraction/agent_cli.py
  - skills/courtage-extraction/SKILL.md
  - skills/specs.md
tests:
  - internal/cmd/courtage_anon_test.go
  - internal/cmd/courtage_graph_test.go
  - skills/courtage-extraction/agent_cli_test.py
decisions:
  - "2026-08-06 : besoin #2 ENCHAÎNÉ sur le besoin #1 dans le MÊME run (choix explicite de l'utilisateur, cadré avant de coder) — kern-orch n'a pas de mécanisme de partage d'état entre deux runs distincts, donc 'un seul flux dossier -> mémo' veut dire rester dans le même graphe plutôt que deux skills séparés."
  - "2026-08-06 : les notes du premier entretien arrivent via POST /api/v1/runs/{id}/nudge pendant la pause à confirm_extraction — réutilise un mécanisme déjà existant (mailbox.Nudge) plutôt que d'inventer un nouveau type de nœud pour 'attendre une entrée texte'."
  - "2026-08-06 : même discipline de masquage PII que le besoin #1 (cadré et confirmé par l'utilisateur avant de coder), mais avec ses PROPRES clés d'état — anonymizePII/deanonymizePII refactorés en newAnonymizeTool/newDeanonymizeTool paramétrées par nom de clé, pour que le second masquage ne clobber jamais extracted_text/masked_text/interpretation du besoin #1, qui doivent survivre dans l'état final à côté du mémorandum."
  - "2026-08-06 : BUG RÉEL trouvé en vérifiant en direct (premier dispatch) — claude -p a répondu avec un bloc JSON fenced SUIVI de texte libre, alors que le prompt dit 'réponds UNIQUEMENT avec un objet JSON'. json.loads a échoué sur la prose de fin ('Extra data'). Corrigé par extract_json_object() (json.JSONDecoder().raw_decode sur le premier '{' trouvé, ignore tout ce qui suit) au lieu d'un json.loads() sur toute la chaîne nettoyée des fences. Même leçon générique que le bug de détection de plateforme Telegram/X : ne jamais supposer qu'une seule sortie de modèle observée couvre toutes les formulations réelles."
---

**Quoi** : besoin #2 de l'agence de courtage — copilote de mémorandum. Génère un draft de
Mémorandum de Financement à partir du dossier extrait (besoin #1) et des notes du premier
entretien, avec la même discipline de masquage PII, enchaîné dans le même run que
l'extraction plutôt qu'un skill séparé. Pipeline complet ajouté au graphe existant :
`extraction_validee → memo_prep → masquage_memo → redaction_memo → demasquage_memo →
confirm_memo`.

**Vérifié en réel** : `go test ./...` vert, `pytest` vert (22 tests, dont la régression du
bug JSON+prose trouvé en direct). Un dispatch HTTP complet de bout en bout : PDF réel →
extraction → masquage → interprétation → `confirm_extraction` en pause → nudge
`notes_entretien` réel → approbation → `memo_prep` → masquage → `claude -p` a rédigé un
draft de mémorandum honnête (statuts "à vérifier" respectés, aucune donnée inventée,
mention explicite des pièces manquantes) → `confirm_memo` en pause → approbation → run
`done`. Vérifié que `interpretation` (besoin #1) survit intact dans l'état final aux côtés
de `memo_draft` (besoin #2) — aucun écrasement.

**Pièges** : voir décision "BUG RÉEL" ci-dessus. Généralisation ajoutée : toute validation
de sortie structurée d'un modèle (JSON, ou tout autre format) doit tolérer du texte
parasite autour de la structure attendue, pas seulement les cas déjà observés (fences
markdown) — une instruction de prompt ("réponds UNIQUEMENT avec...") n'est jamais une
garantie.
