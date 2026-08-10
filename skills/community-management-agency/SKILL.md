---
name: community-management-agency
type: agent
description: Pilote la communication (prospection écrite et posts de référencement) en langage naturel — brief audience, avis ou proposition de stratégie, rédaction par plateforme, deux validations humaines (stratégie proposée, publication)
graph: examples/community-management-agency.yaml
---

# community-management-agency

Dispatché depuis le chat (`/community-management-agency <texte en langage naturel>`).
Comme `prospection`, ce skill déclare un `graph:` — Dispatch charge
`examples/community-management-agency.yaml` au lieu de construire un run à un nœud, et
le texte du chat devient `state["message"]` avant que le premier nœud (`audience`) ne
s'exécute.

Nécessite `KERN_AGENT_CLI` pointé sur
`skills/community-management-agency/agent_cli.py` — un seul script, une branche par
nœud (`audience`, `strategiste`, `redacteur`, `publieur`), qui réutilise en bibliothèque
les prompts déjà écrits dans
`mon-orchestrateur-agents/agents-locaux/crew-comm` (`agents/audience.py`,
`agents/strategiste.py`, `agents/redacteur.py`), exactement comme `prospection` réutilise
`crew-crm`. Exécute chaque nœud via le vrai CLI `claude` (Claude Code), même technique
que `prospection/agent_cli.py`.

## Double mode côté stratège

`crew-comm` n'avait qu'un mode (le stratège propose toujours). Ce skill en ajoute un
second, en instruction supplémentaire au prompt du stratège (pas en modifiant
`crew-comm/agents/strategiste.py`) :
- **avis** — l'utilisateur a déjà fourni sa propre stratégie dans sa demande : le
  stratège se limite à un avis court (5 lignes max), et le graphe (`onStrategyMode`,
  `internal/cmd/comm_routers.go`) saute directement à `redacteur`, sans validation —
  c'est la décision de l'utilisateur, pas une proposition du harnais.
- **proposition** (défaut si le marqueur est absent ou non reconnu) — le stratège
  construit la stratégie lui-même, et un humain doit la valider (`confirm_strategie`,
  approval) avant toute rédaction.

## Deux validations humaines

- `confirm_strategie` — sautée en mode avis, obligatoire en mode proposition.
- `confirm_publication` — **toujours obligatoire**, jamais sautée : c'est la seule action
  externe réelle du graphe. Sans connecteur de publication branché (aucun MCP câblé pour
  l'instant), `run_publieur` ne fait aucun appel modèle et signale explicitement que rien
  n'a été publié (même garde-fou G2 que `crew-comm/agents/publieur.py`).

## Canaux couverts

- **Réseaux sociaux** (LinkedIn, Instagram, X, TikTok) — d'origine dans le prompt du
  stratège de `crew-comm`. LinkedIn seul vérifié en réel pour l'instant.
- **Email froid / outreach** (2026-08-06) — ajouté par instruction, `crew-comm` ne le
  connaissait pas nativement. Objet + corps court, éventuellement plusieurs touches
  espacées dans le temps. Le squelette de plan reste "Publier"/"Programmer"/"Planifier"
  (celui de `crew-comm/agents/redacteur.py`, non dupliqué) — un email se dit donc
  "Publier l'email..." plutôt que "Envoyer l'email...", pour ne pas casser l'extraction du
  plan partagée avec le canal social.
- **Telegram** (2026-08-06) — premier canal avec un vrai connecteur. `run_publieur`
  détecte "Telegram" dans la ligne "Plateforme(s) :" du brief et, si la publication est
  approuvée et que les identifiants Telegram sont configurés dans l'environnement du
  process Python, envoie réellement le texte rédigé via l'API Bot Telegram (module
  standard `urllib`, aucune dépendance ajoutée). Sans détection ou sans identifiants, le
  garde-fou habituel s'applique comme avant. La validation humaine reste le seul geste qui
  déclenche l'envoi.
- **X** (2026-08-06) — deuxième canal avec un vrai connecteur (API v2, signature OAuth
  1.0a écrite à la main avec la bibliothèque standard, aucune dépendance ajoutée). Limite
  de 280 caractères vérifiée côté code avant l'appel réseau (échec explicite plutôt qu'un
  400 confus de l'API). Un seul post, pas de thread pour l'instant. Même contrat que
  Telegram : sans identifiants, garde-fou habituel.
- **Newsletter / blog** (2026-08-06) — format long (500-1500 mots), objet (si
  newsletter) + titre honnête + introduction + sections à sous-titres + conclusion avec
  appel à l'action. Pas de connecteur réel, comme LinkedIn/Instagram/TikTok.
- **Instagram / TikTok** — toujours en mode "propose, l'humain publie", volontairement.
  Contrainte réelle de plateforme, pas un oubli : l'API Graph d'Instagram et l'API de
  publication TikTok exigent toutes les deux une image ou une vidéo, aucune n'accepte un
  post texte seul. Un vrai connecteur pour ces deux-là suppose une brique de génération
  d'image en amont (piste évoquée, non construite : `kern-image`, ou un skill dédié qui
  appelle un service tiers type Higgsfield / GPT-image) — hors périmètre de ce skill tant
  qu'elle n'existe pas.

## Vérifié

Logique pure vérifiée en isolation (`extract_mode`/`strip_mode_line`, `run_publieur` avec
et sans approbation, `_extraire_plan` importé de `crew-comm` sur un plan d'exemple) — voir
`docs/index/0023-community-management-agency.md`. Le pipeline complet contre le vrai CLI
`claude` (les quatre nœuds enchaînés, les deux modes stratège, la publication) reste à
vérifier en conditions réelles, comme `prospection` l'a été après son propre scaffold.
