---
name: community-management-agency-auto
type: agent
description: "⚠️ Variante automatique de community-management-agency — publie SANS validation humaine sur Telegram/X (les seuls canaux avec un vrai connecteur), à n'utiliser qu'en connaissance de cause. Sur tout autre canal, comportement identique au skill standard (validation requise)."
graph: examples/community-management-agency-auto.yaml
---

# community-management-agency-auto

⚠️ **Lisez ceci avant d'utiliser ce skill.** C'est une variante volontairement distincte
de [`community-management-agency`](../community-management-agency/SKILL.md) — jamais le
comportement par défaut, jamais activée sans que vous ayez tapé explicitement
`/community-management-agency-auto`. Utiliser `/community-management-agency` (sans
`-auto`) garde toujours une validation humaine avant toute publication, quel que soit le
canal.

## Ce que "auto" change, précisément

Rien ne change avant la rédaction : brief d'audience, stratégie (avec sa propre
validation humaine si le stratège la propose lui-même), rédaction — identiques au skill
standard.

**Ce qui change : la validation `confirm_publication`.** Elle est sautée automatiquement
— publication immédiate, sans qu'un humain relise le texte final avant l'envoi — mais
**uniquement si le canal choisi par le stratège a un vrai connecteur d'envoi** :
aujourd'hui, Telegram et X. Sur tout autre canal (LinkedIn, Instagram, TikTok, email,
newsletter/blog), il n'existe de toute façon aucun connecteur réel : le graphe route vers
`confirm_publication` exactement comme le skill standard, et rien n'est publié sans
validation.

**Concrètement, un post Telegram ou X généré par ce skill part réellement, sans que vous
en ayez vu le texte final avant l'envoi.** Le seul filet de sécurité restant à ce stade
est la qualité du brief validé (ou de la stratégie que vous avez vous-même fournie) —
relisez-le avant de lancer ce skill, pas après.

## Détection du canal

`internal/cmd/comm_auto.go` (`onAutoPublishRoute`) lit la ligne "Plateforme(s) :" du
brief éditorial du stratège — même logique que `TELEGRAM_PLATFORM_RE`/`X_PLATFORM_RE`
côté Python (`agent_cli.py`), dupliquée à la main faute de pouvoir partager une regex
entre Go et Python. Si un futur canal obtient un vrai connecteur, les deux détections
doivent être mises à jour ensemble.

## Adaptateur

Réutilise `skills/community-management-agency/agent_cli.py` tel quel (`run.sh` pointe
sur le même script) — `run_publieur` n'a besoin d'aucune modification : le nœud Go
`auto_approve` inscrit la même clé d'état (`decision:confirm_publication` = `approve`)
qu'une validation humaine aurait laissée, donc `publieur` ne peut pas distinguer les deux
chemins et n'a pas à le faire.
