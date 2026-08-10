---
id: 0018
feature: daemon-mode
branch: feature/daemon-mode
status: done
files:
  - internal/daemon/router.go
  - internal/cmd/serve.go
  - internal/cmd/commands.go
  - internal/checkpoint/store.go
  - internal/config/config.go
tests:
  - internal/daemon/router_test.go
  - internal/cmd/daemon_runner_test.go
  - internal/cmd/exposure_test.go
decisions:
  - "2026-07-28 : `internal/daemon` ne connaît RIEN des graphes/checkpoints/reporters — une interface `Runner`, testée avec un faux. `internal/cmd` l'implémente avec le câblage existant"
  - "2026-07-28 : `StartRun` échoue de façon SYNCHRONE sur un chemin absent ou un graphe invalide — sinon l'appelant interrogerait indéfiniment un run qui n'apparaîtra jamais"
  - "2026-07-28 : marqueur `queued` (step -1) écrit AVANT de répondre — une requête de statut juste après l'acceptation ne doit jamais tomber sur un 404"
  - "2026-07-28 : `prepareRun`/`preparedRun.run` extraits pour que `run`, `resume` ET le démon partagent LA MÊME implémentation — pas une troisième copie du câblage reporter/activité"
  - "2026-07-28 : refus de démarrer sur une adresse publique sans jeton — même règle que kern-ui, RE-DÉRIVÉE et non partagée (indépendance des briques)"
  - "2026-07-28 : reprendre un run déjà terminé est un NO-OP, pas une erreur — cohérent avec le message « already complete » du CLI"
---

**Quoi** : `kern-orch serve`. Un service qui tourne, accepte des runs par HTTP, les exécute
en tâche de fond. Prérequis d'EPIC-03 (exposition des tools), pas EPIC-03 lui-même — aucun
tool n'est encore lisible depuis l'extérieur, seuls les runs le sont.

**Refactor imposé par l'ajout** : `run` et `resume` dupliquaient déjà tout le câblage
(reporter, activité, sous-graphes). Ajouter un troisième appelant sans factoriser aurait
rendu trois copies désynchronisables. Extrait en `prepareRun` (construit et valide,
synchrone) + `preparedRun.run` (exécute, flush, rapporte l'échec) — les trois surfaces
appellent maintenant la même implémentation.

**Vérifié en réel** : démon lancé en local, run démarré par HTTP, statut interrogé
immédiatement (queued → done), liste, reprise sur run inconnu (404), chemin invalide refusé
de façon synchrone (400), adresse publique sans jeton refusée au démarrage.
