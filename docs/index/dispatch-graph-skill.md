---
id: 0007
feature: dispatch-graph-skill
branch: feature/dispatch-graph-skill
status: done
files:
  - internal/skills/skills.go
  - internal/cmd/serve.go
  - examples/prospection.yaml
  - skills/prospection/SKILL.md
  - skills/prospection/agent_cli.py
  - skills/crm-dashboard/SKILL.md
  - skills/crm-dashboard/tool.py
tests:
  - internal/cmd/dispatch_test.go (TestDaemonRunnerDispatchWithAGraphLoadsTheFileAndNudgesTheMessage)
decisions:
  - "2026-07-31 : skills.Skill gagne Graph string (yaml:graph,omitempty) — un skill agent dispatché depuis le chat peut charger un vrai fichier topology au lieu de l'ancien run à un seul nœud ad-hoc. Vide = comportement inchangé (planner)."
  - "2026-07-31 : le texte du chat devient l'entrée du premier nœud via le mécanisme de nudge déjà existant (mailbox.Nudge(\"message\", text) avant de lancer la goroutine) — aucune nouvelle primitive, juste une réutilisation du C6 de cette session."
  - "2026-07-31 : pipeline démo secretaire → commercial → confirm (approval) → approved/refused, réutilisant le router onConfirmDecision déjà câblé pour steer.yaml — l'agent 'expert' est nommé 'approved' pour correspondre au routeur existant, aucun changement Go supplémentaire."
  - "2026-07-31 : skills/prospection/agent_cli.py réutilise en BIBLIOTHÈQUE la config/les prompts déjà rendus agnostiques au modèle dans mon-orchestrateur-agents/agents-locaux/crew-crm (session du même jour) — Kern est la démo, crew-crm une source de logique prouvée, pas un système parallèle."
  - "2026-07-31 : DÉCOUVERTE EN TESTANT — mcp_client.charger_tools() et d'autres print() de crew-crm polluent stdout, cassant le protocole JSON-lines d'agentrunner. Corrigé en redirigeant fd 1 vers fd 2 pendant l'exécution du nœud, restauré (après flush) avant d'écrire la ligne de résultat réelle."
  - "2026-07-31 : DÉCOUVERTE EN TESTANT — Gemini (LangChain) retourne .content comme une LISTE de blocs, pas une chaîne (contrairement à Ollama/Anthropic) — normalisé par une fonction texte() locale à l'adaptateur plutôt que de supposer un format uniforme."
  - "2026-07-31 : DÉCOUVERTE EN TESTANT — la clé Gemini de démo a un quota gratuit d'environ 20 requêtes/jour sur le modèle courant, à zéro sur les autres générations testées. Accepté comme risque connu (décision utilisateur) ; secours documenté : MODEL_BACKEND=ollama sans changement de code."
---

**Quoi** : `Dispatch` peut charger un vrai graphe multi-nœuds pour un skill agent
(`skills.Skill.Graph`), au lieu du run à un seul nœud ad-hoc qu'il construisait
systématiquement. Le skill `prospection` en est le premier exemple réel : un pipeline fixe
secrétaire → commercial → validation humaine → exécution, piloté par
`/prospection <texte en langage naturel>` depuis le chat kern-ui. Un second skill,
`crm-dashboard` (type tool), alimente l'Espace avec une vraie statistique du CRM en
direct.

**Vérifié en réel** : `go test ./...` vert, y compris un nouveau test qui prouve que
`Dispatch` charge le bon fichier et que le texte du chat atteint bien le premier nœud via
le nudge, avant que celui-ci ne s'exécute. `skills/crm-dashboard/tool.py` vérifié contre
le vrai serveur MCP du CRM (vraie valeur de pipeline reçue). `skills/prospection/agent_cli.py`
vérifié nœud par nœud contre le vrai backend Gemini : `secretaire` (fiche lead réelle,
~12s), `commercial` (plan d'action réel extrait, ~19s) — la branche `expert`
(écriture réelle dans le CRM) reste à vérifier lors de la répétition, une fois le quota
Gemini journalier réinitialisé.

**Pièges** : trois découverts uniquement en testant contre le vrai backend, aucun
prévisible en lisant le code — la pollution de stdout, le format de contenu Gemini, et le
quota gratuit quasi inexistant sur les modèles autres que le courant. Tous documentés
dans `skills/prospection/SKILL.md` et `agent_cli.py` directement, pas seulement ici.
