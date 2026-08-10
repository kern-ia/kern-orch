---
id: 0032
feature: courtage-banques
branch: feature/courtage-banques
status: done
files:
  - examples/courtage-banques.yaml
  - skills/courtage-banques/agent_cli.py
  - skills/courtage-banques/run.sh
  - skills/courtage-banques/SKILL.md
  - skills/specs.md
tests:
  - internal/cmd/courtage_banques_graph_test.go
  - skills/courtage-banques/agent_cli_test.py
decisions:
  - "2026-08-07 : SKILL SÉPARÉ, jamais un nœud ajouté à courtage-extraction — confirmé par l'utilisateur avant de coder. Colle au besoin réel (« agent conversationnel interne interrogé en langage naturel », pas rattaché à un dossier précis), contrairement à l'extraction qui traite un dossier client donné."
  - "2026-08-07 : lecture seule, aucune validation humaine — pas d'effet externe réel (contrairement à la publication de community-management-agency ou même à la notification interne de courtage-relance), rien à approuver."
  - "2026-08-07 : sans kern-memory joignable, erreur claire plutôt qu'une réponse inventée en repli — il n'existe pas de « moteur local de secours » pour du RAG comme il en existe un pour l'OCR (Tesseract local) : sans mémoire, il n'y a rien à interroger."
  - "2026-08-07 : le skill n'écrit JAMAIS dans kern-memory — lecture seule stricte. Les vrais critères banques doivent être fournis par l'équipe AvelFinances et écrits séparément (POST /api/v1/memory/write), jamais fabriqués par ce skill ni halluciné par Claude."
---

**Quoi** : besoin #4 de l'agence de courtage — agent conversationnel RAG sur les critères
d'octroi des banques partenaires. Skill séparé `courtage-banques`, un seul nœud : interroge
`kern-memory` (EPIC-13 phase 1, tout juste construit) puis synthétise une réponse sourcée
avec `claude -p`, à partir UNIQUEMENT des extraits retournés.

**Vérifié en réel** : `go test ./...` et `pytest` verts (8 tests Python, HTTP mocké). Un
vrai dispatch HTTP de bout en bout, deux daemons réels (`kern-memory` avec deux critères
banques de test écrits via son API, `kern-orch` avec cet adaptateur) : la question exacte
du prospect (« Quelle banque accepte un prêt hypothécaire sur un bien en SCI avec un
emprunteur de 74 ans ? ») a produit une réponse correcte, sourcée par identifiant pour
chaque affirmation, correctement synthétisant deux critères contradictoires (l'un accepte,
l'autre refuse) — et le modèle a spontanément signalé que les données étaient marquées
« [DONNÉE DE TEST] » et ne devaient pas être traitées comme réelles, sans qu'aucune
instruction explicite ne le lui demande. Run terminé (`dispatched run completed`).

**Pièges** : aucun nouveau. Ça referme le découpage court terme de l'agence de courtage
(specs.md) : besoins #1 à #4 tous construits et vérifiés en réel ; le besoin #5
(soumission bancaire automatisée + conformité) reste explicitement hors scope.
