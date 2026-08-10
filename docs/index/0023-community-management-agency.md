---
id: 0023
feature: community-management-agency
branch: feature/community-management-agency
status: done
files:
  - internal/cmd/comm_routers.go
  - internal/cmd/runtime.go
  - examples/community-management-agency.yaml
  - skills/community-management-agency/agent_cli.py
  - skills/community-management-agency/SKILL.md
  - skills/community-management-agency/run.sh
tests:
  - internal/cmd/comm_routers_test.go
  - internal/cmd/comm_graph_test.go
decisions:
  - "2026-08-05 : onConfirmDecision (codé en dur sur un nœud \"confirm\" et les cibles \"approved\"/\"refused\") n'est pas réutilisable pour ce graphe, qui a DEUX points d'approbation. decisionRouter(nodeID, approved, refused) généralise le motif, testé en isolation, câblé deux fois (confirm_strategie, confirm_publication) sans toucher au routeur existant."
  - "2026-08-05 : double mode côté stratège (avis / proposition) porté par un marqueur littéral \"MODE: avis|proposition\" que le prompt force en tête de réponse, parsé côté adaptateur (extract_mode) — pas de heuristique sur le message brut de l'utilisateur, le jugement reste au modèle. Défaut sûr si le marqueur manque : \"proposition\" (jamais sauter la validation humaine par erreur)."
  - "2026-08-05 : crew-comm réutilisé en bibliothèque (agents/audience.py, strategiste.py, redacteur.py) exactement comme prospection réutilise crew-crm — _extraire_plan importé tel quel (privé par convention, réutilisé quand même, même précédent que _marquer_inventions)."
  - "2026-08-05 : run_publieur reproduit le garde-fou G2 de crew-comm/agents/publieur.py à l'identique — sans connecteur de publication branché, aucun appel modèle, signalement explicite plutôt qu'un faux compte-rendu."
  - "2026-08-05 : E2E réel fait dans kern-ui (dispatch /community-management-agency depuis le chat, les deux validations humaines cliquées en vrai) — bin/kern-ui était périmé (28/07, sans la route POST /api/v1/dispatch de C6) et répondait 404 puis 405 ; rebuild (make build) a résolu, sans rapport avec le code de ce skill."
---

**Quoi** : nouveau skill `community-management-agency` — brief audience → avis ou
proposition de stratégie → (validation humaine si proposition) → rédaction par
plateforme → validation humaine → publication. Port de `crew-comm` (LangGraph/Ollama)
dans un graphe kern-orch, sur le patron de `prospection`.

**Vérifié en réel** : dispatch depuis le chat kern-ui (`/community-management-agency
Rédige un post LinkedIn...`), les quatre nœuds enchaînés contre le vrai `claude` CLI, les
deux modes de routage exercés (mode "proposition" pris faute de stratégie fournie →
`confirm_strategie` a bloqué comme prévu), les deux validations humaines cliquées dans le
navigateur. `redacteur` a extrait un plan de publication propre avec `[À COMPLÉTER]`
plutôt qu'un chiffre inventé pour le quota de testeurs. `publieur` a correctement refusé
de prétendre publier sans connecteur branché (garde-fou G2). `go test ./... -race` vert.

**Pièges** : `bin/kern-ui` était un binaire périmé (28/07) sans la route de dispatch C6 —
404 puis 405 en test, résolu par `make build`. Sans rapport avec ce skill, mais bloquant
tant qu'on ne le sait pas : si un dispatch échoue en 404/405 depuis kern-ui, vérifier
l'âge du binaire avant de chercher le bug côté skill.
