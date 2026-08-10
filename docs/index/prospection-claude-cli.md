---
id: 0008
feature: prospection-claude-cli
branch: feature/prospection-claude-cli
status: done
files:
  - skills/prospection/agent_cli.py
  - skills/prospection/SKILL.md
tests: []
decisions:
  - "2026-07-31 : abandon du backend Gemini/LangChain pour l'adaptateur KERN_AGENT_CLI — quota gratuit de la clé de démo ~20 requêtes/jour sur le seul modèle viable, zéro sur tous les autres testés (voir docs/index/dispatch-graph-skill.md pour le détail de cette découverte)."
  - "2026-07-31 : remplacé par le vrai CLI `claude` (Claude Code), invoqué non-interactivement (`claude -p ... --mcp-config ... --allowedTools ...`) — utilise l'authentification déjà en place, parle MCP nativement, fait sa propre boucle d'appel d'outils. L'adaptateur n'a donc plus besoin de LangChain/boucle_react/config.py de crew-crm — seulement des constantes de prompt (texte pur)."
  - "2026-07-31 : subprocess.run(capture_output=True) élimine le risque de pollution du stdout JSON-lines qui existait avec l'approche précédente (le fd-redirect n'est plus nécessaire — ce que `claude` écrit reste dans le sous-processus, jamais sur le stdout de l'adaptateur)."
  - "2026-07-31 : READ_TOOLS explicite (noms littéraux) pour secretaire — --allowedTools de claude ne supporte pas le même filtrage par suffixe que mcp_client.py de crew-crm ; approved reçoit le catalogue CRM complet (mcp__crm__*), le geste humain de validation ayant déjà eu lieu avant ce nœud."
---

**Quoi** : remplacement du backend de raisonnement de `skills/prospection/agent_cli.py` —
Gemini/LangChain remplacé par le vrai CLI `claude`, appelé en sous-processus par nœud.
Chaque nœud (`secretaire`, `commercial`, `approved`) devient un appel `claude -p` avec son
propre jeu d'outils autorisés (`--allowedTools`) et, pour les nœuds qui touchent le CRM, un
`--mcp-config` généré à la volée depuis les variables d'environnement du processus.

**Vérifié en réel, les trois nœuds, en une seule passe** : `secretaire` (~15s), `commercial`
(~61s, plan d'action réel extrait), `approved` (~22s, écriture réelle dans le CRM — une
société de test "Cheval Blanc SAS" créée, et un refus correct d'exécuter une seconde
action pour donnée manquante, conforme à la consigne anti-hallucination du prompt expert
repris tel quel de crew-crm). Pipeline complet hors validation humaine : ~1min40, sans
aucun risque de quota.

**Pièges évités par construction plutôt que corrigés après coup** : la pollution de stdout
et le format de contenu variable par fournisseur (les deux pièges trouvés avec l'approche
LangChain précédente, voir `dispatch-graph-skill.md`) n'existent plus du tout avec cette
architecture — `claude -p` répond en texte simple sur son propre stdout, capturé
proprement par `subprocess.run`.
