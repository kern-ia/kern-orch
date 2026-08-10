---
name: prospection
type: agent
description: Pilote une prospection CRM en langage naturel — fiche lead, plan d'action, validation humaine, exécution
graph: examples/prospection.yaml
---

# prospection

Dispatché depuis le chat (`/prospection <texte en langage naturel décrivant le lead>`).
Contrairement à un agent skill ordinaire (un seul nœud ad-hoc, tout le texte comme
prompt), ce skill déclare un `graph:` — Dispatch charge le fichier
`examples/prospection.yaml` au lieu de construire un run à un nœud, et le texte du chat
devient `state["message"]` avant que le premier nœud (`secretaire`) ne s'exécute.

Nécessite `KERN_AGENT_CLI` pointé sur `skills/prospection/agent_cli.py` — un seul
script, une branche par nœud (`secretaire`, `commercial`, `approved`), qui réutilise en
bibliothèque les prompts (texte pur, sans dépendance modèle) déjà écrits dans
`mon-orchestrateur-agents/agents-locaux/crew-crm`.

**2026-07-31, décision finale** : l'adaptateur exécute chaque nœud via le vrai CLI
`claude` (Claude Code) — `claude -p "<prompt>" --mcp-config <crm> --allowedTools <...>`
— pas une clé API séparée. Abandon d'un premier essai Gemini/LangChain : le quota
gratuit de la clé de démo s'est révélé à ~20 requêtes/jour sur le seul modèle viable,
zéro sur tous les autres testés. `claude` utilise l'authentification déjà en place, parle
MCP nativement et fait sa propre boucle d'appel d'outils — l'adaptateur n'a donc plus
besoin de LangChain, seulement des textes de prompt de crew-crm.

**Vérifié en vrai, les trois nœuds, en une passe** : `secretaire` (~15s, fiche lead
correcte, vérification CRM réelle), `commercial` (~61s, plan d'action réel extrait),
`approved` (~22s, écriture réelle dans le CRM — société de test créée, ET refus correct
d'une action pour donnée manquante, conforme à la consigne anti-hallucination du prompt
expert). Pipeline complet (hors temps de validation humaine) : environ 1min40.

**Trace laissée dans le CRM par cette vérification** : une société de test
"Cheval Blanc SAS" (id `cms95n4j4002901nx9ydia6td`) — à supprimer ou garder comme
donnée de démo, au choix.
