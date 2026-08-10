---
id: 0028
feature: comm-auto-mode
branch: feature/comm-auto-mode
status: done
files:
  - internal/cmd/comm_auto.go
  - internal/cmd/runtime.go
  - examples/community-management-agency-auto.yaml
  - skills/community-management-agency-auto/SKILL.md
  - skills/community-management-agency/agent_cli.py
tests:
  - internal/cmd/comm_auto_test.go
  - internal/cmd/comm_graph_test.go
  - skills/community-management-agency/agent_cli_test.py
decisions:
  - "2026-08-06 : mode auto livré comme un SKILL SÉPARÉ (community-management-agency-auto, commande /community-management-agency-auto), jamais un réglage du skill standard — respecte la contrainte posée avant de commencer : explicite, jamais le défaut. /community-management-agency (sans -auto) garde toujours une validation humaine, quel que soit le canal."
  - "2026-08-06 : la publication ne saute la validation humaine QUE si le canal choisi a un vrai connecteur (Telegram, X aujourd'hui) — sur tout autre canal, le graphe auto route vers confirm_publication exactement comme le graphe standard, parce qu'il n'existe de toute façon aucun connecteur réel à déclencher sans validation."
  - "2026-08-06 : le nœud Go auto_approve inscrit la même clé d'état (decision:confirm_publication = approve) qu'une validation humaine — publieur (Python, agent_cli.py) n'a eu besoin d'AUCUNE modification pour supporter le chemin automatique, il ne peut pas distinguer les deux."
  - "2026-08-06 : BUG RÉEL trouvé en vérifiant en direct (premier dispatch réel) — le stratège a écrit 'Plateforme : Telegram' (SINGULIER, sans '(s)') cette fois, alors que toutes les détections précédentes (Telegram, X) supposaient littéralement 'Plateforme(s)'. Le premier essai est tombé sur confirm_publication au lieu du chemin automatique — comportement sûr par construction (retombe sur la validation humaine), mais le mode auto ne s'est simplement pas déclenché. Corrigé des DEUX côtés (Go onAutoPublishRoute ET Python TELEGRAM_PLATFORM_RE/X_PLATFORM_RE) avec plateforme(\\(s\\))? au lieu de plateforme\\(s\\), tests de non-régression des deux côtés."
---

**Quoi** : nouveau skill `community-management-agency-auto` — variante du skill standard
où la publication part sans validation humaine, mais uniquement sur un canal avec un vrai
connecteur (Telegram, X). Skill distinct, jamais le comportement par défaut ; le skill
standard n'a pas changé.

**Vérifié en réel, deux passes** : la première a révélé le bug "Plateforme :" sans "(s)"
— comportement sûr (retombé sur validation humaine) mais le mode auto ne s'est pas
déclenché, donc pas vérifié tel quel. Corrigé, redéployé, seconde passe : le graphe a
routé `redacteur → auto_approve → publieur` sans jamais activer `confirm_publication`
(resté "en attente" tout du long), et un vrai message est parti sur Telegram
(`message_id 42`), confirmé dans l'interface. `go test ./...` vert (19 tests dans
`internal/cmd` touchant ce skill), `pytest` vert (25 tests Python, y compris la
régression "sans (s)").

**Pièges** : voir décision "BUG RÉEL" — la détection de plateforme dépend du phrasé exact
du modèle, qui varie d'un tour à l'autre ("Plateforme(s)" vs "Plateforme"). Générique à
surveiller : toute détection de contenu basée sur un texte généré par un modèle doit
tester plusieurs phrasés réels, pas un seul exemple observé une fois.
