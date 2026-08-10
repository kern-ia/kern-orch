---
id: 0022
feature: c6-dispatch
branch: feature/c6-dispatch
status: done
files:
  - internal/daemon/router.go
  - internal/cmd/serve.go
  - skills/planner/SKILL.md
tests:
  - internal/daemon/router_test.go
  - internal/cmd/dispatch_test.go
decisions:
  - "2026-07-29 : périmètre élargi en cours de cadrage — lancer un run depuis le chat (queue) rentre dans C6 v1, plutôt que reporté à une session de cadrage séparée."
  - "2026-07-29 : V1 = commande explicite `/skill texte…` matchée sur le catalogue de skills — pas d'interprétation en langage naturel ni de résolution d'ambiguïté (nommé comme direction future, pas construit)."
  - "2026-07-29 : un skill `type: tool` sans param requis, ou avec exactement un, se prête à la commande ; plus d'un param requis est refusé plutôt que deviné."
  - "2026-07-29 : un skill `type: agent` n'a pas de gabarit de prompt réutilisable — le texte du chat EST le prompt entier d'un run à un seul nœud, construit en mémoire (aucun fichier YAML)."
  - "2026-07-29 : le chemin agent réutilise l'implémentation de StartRun (checkpoint, requester, mailbox) à l'identique — seule la construction du graphe diffère (prepareAdhocRun)."
---

**Quoi** : `POST /api/v1/dispatch` — la moitié « lancer/appeler depuis le chat » de C6.
Un skill `type: tool` délègue directement à l'invocation C5 existante. Un skill
`type: agent` construit un graphe à un seul nœud en mémoire et le lance par le même chemin
d'exécution que `StartRun` (checkpoint, requester, mailbox pour le pilotage). Un nom de
skill inconnu répond 404 avec la liste réelle des skills chargés.

**Vérifié en réel** : `kern-orch serve` + `curl` — `/heartbeat` (aucun argument) et
`/greeting Yoann` (un argument) renvoient une valeur immédiate ; `/planner analyse ceci`
lance un vrai run qui apparaît ensuite dans `GET /runs/{id}` avec son `requester` et le
texte du chat comme prompt réellement reçu par l'agent (stub) ; un nom inconnu liste les
trois skills réels.
