---
id: 0031
feature: courtage-relance
branch: feature/courtage-relance
status: done
files:
  - internal/cmd/courtage_relance.go
  - internal/cmd/runtime.go
  - examples/courtage-extraction.yaml
  - skills/courtage-extraction/agent_cli.py
  - skills/specs.md
tests:
  - internal/cmd/courtage_relance_test.go
  - internal/cmd/courtage_graph_test.go
  - skills/courtage-extraction/agent_cli_test.py
decisions:
  - "2026-08-06 : relance NOTIFIE L'ÉQUIPE INTERNE (Telegram, canal fixe déjà configuré), jamais le client directement — cadré et confirmé par l'utilisateur avant de coder. Le besoin prospect original ('relances au client par SMS/email') suppose un modèle client -> coordonnées qui n'existe pas, et Telegram ne peut de toute façon pas contacter un utilisateur qui n'a pas parlé au bot en premier. L'IA rédige le message prêt à envoyer, l'humain (Cassandra/Auriane) le transmet au client lui-même."
  - "2026-08-06 : onRelanceNeeded combine dans UN SEUL routeur le fan-out inconditionnel vers memo_prep et le branchement conditionnel vers relance_prep/relance_non_necessaire — kern-orch n'autorise qu'un routeur par nœud source (internal/graph/engine.go, routes map[string]RouteFunc), donc pas moyen de déclarer les deux séparément sur le même nœud extraction_validee."
  - "2026-08-06 : pas de validation humaine supplémentaire avant l'envoi de la relance (contrairement à la publication de community-management-agency) — c'est une notification INTERNE, pas une action visible du client ; le principe 'l'IA propose, l'humain décide' porte sur les actions ayant un effet externe réel, pas sur le fait d'alerter un collègue."
  - "2026-08-06 : run_relance_prep ne fabrique jamais de liste de pièces manquantes — une interpretation JSON illisible route directement vers relance_non_necessaire (onRelanceNeeded), et même en défense supplémentaire côté Python, un dossier illisible produit un message générique demandant une vérification manuelle plutôt qu'une liste inventée."
---

**Quoi** : besoin #3 de l'agence de courtage — relance des pièces manquantes. Enchaîné
sur le même run que les besoins #1/#2, en parallèle du mémorandum (pas séquentiel) :
`extraction_validee → [onRelanceNeeded] → relance_prep → relance_notify` (ou
`relance_non_necessaire` si le dossier est complet), simultané à `memo_prep → ...`.
Réutilise le tool `notify` déjà construit (Telegram interne) — aucun nouveau connecteur.

**Vérifié en réel** : `go test ./...` et `pytest` verts (24 tests Python, 5 tests Go pour
le routeur). Un vrai dispatch HTTP de bout en bout avec un dossier ayant des pièces
manquantes : `confirm_extraction` approuvé → `message` correctement formaté (liste réelle
des pièces manquantes issues du dossier interprété, nom du fichier source) → envoi
Telegram réel déclenché en parallèle du mémorandum → run terminé sans erreur
(`dispatched run completed`, aucune ligne `ERROR` — le chemin d'erreur du tool `notify`
avait déjà été vérifié capable de logger une vraie erreur lors du bug JSON du besoin #2,
donc son silence ici est un signal fiable de succès).

**Pièges** : aucun nouveau — le seul point notable est architectural (un seul routeur par
nœud source, cf. décision ci-dessus), pas un bug trouvé en direct comme les précédents.
