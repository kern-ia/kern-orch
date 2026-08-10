---
id: 0021
feature: c6-steer
branch: feature/c6-steer
status: done
files:
  - internal/graph/node.go
  - internal/graph/engine.go
  - internal/steer/mailbox.go
  - internal/checkpoint/store.go
  - internal/checkpoint/sqlite.go
  - internal/topology/loader.go
  - internal/daemon/router.go
  - internal/cmd/runtime.go
  - internal/cmd/serve.go
  - internal/cmd/commands.go
  - examples/steer.yaml
  - examples/wait.yaml
  - examples/nudge.yaml
tests:
  - internal/graph/node_test.go
  - internal/graph/engine_test.go
  - internal/steer/mailbox_test.go
  - internal/checkpoint/requester_test.go
  - internal/topology/loader_test.go
  - internal/daemon/router_test.go
  - internal/cmd/daemon_runner_test.go
decisions:
  - "2026-07-29 : approval = un vrai nœud bloquant (ApprovalNode.Execute attend), pas une annotation a posteriori — réutilise le concurrency model existant (runLevel attend déjà le nœud le plus lent), aucun changement au cœur du moteur."
  - "2026-07-29 : nudge s'applique ENTRE deux niveaux (Engine.OnBeforeLevel) — le seul point où rien d'autre ne touche le State, donc sûr sans verrou supplémentaire."
  - "2026-07-29 : stop réutilise le chemin d'échec existant — annuler le contexte du run tue un nœud subprocess en cours (agentrunner utilise déjà exec.CommandContext), aucune mécanique nouvelle."
  - "2026-07-29 : Requester vide = run ouvert, pilotable par n'importe quel acteur — garde les runs CLI d'aujourd'hui pilotables sans rien changer, et se resserre dès qu'un appelant (kern-ui) fournit un vrai demandeur."
  - "2026-07-29 : bac à sable CLI (`run`/`resume`) reste sans mailbox — un graphe avec un nœud approval refuse de charger plutôt que de bloquer indéfiniment sans rien pour répondre."
  - "2026-07-29 : bug de concurrence réel découvert en construisant ceci — StopRun/Nudge/Decide lisent le checkpoint pendant qu'un run l'écrit, ce que rien ne faisait avant. Corrigé : SetMaxOpenConns(1) + PRAGMA busy_timeout sur le store SQLite (dette notée depuis le bootstrap, payée ici)."
---

**Quoi** : C6 v1, moitié pilotage d'un run déjà en cours. Trois opérations, confirmées le
2026-07-28 : `POST /runs/{id}/stop` (annule le contexte du run), `POST /runs/{id}/nudge`
(injecte une clé d'état pour le niveau suivant), `POST /runs/{id}/nodes/{node}/decide`
(débloque un nœud `type: approval`, qui route ensuite via un router conditionnel ordinaire
lisant `graph.DecisionKey(node)`). Chaque opération vérifie l'acteur contre le `Requester`
du run — vide veut dire ouvert.

**Vérifié en réel** : `kern-orch serve` + `curl` — un acteur refusé reçoit 403, le vrai
demandeur débloque une approbation et le run route correctement ; un nœud bloquant est
réellement interrompu par `stop` ; une valeur poussée par `nudge` pendant l'exécution
atterrit dans l'état du niveau suivant.

**Pièges** : la concurrence SQLite notée comme dette depuis le tout premier bootstrap
(`retro.md`, 2026-07-20) a fini par mordre ici — c'est la première fonctionnalité qui lit
le store pendant qu'un run l'écrit activement.
