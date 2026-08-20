---
id: 0057
feature: adapter-lifecycle-port
branch: feature/adapter-lifecycle-port
status: done
files:
  - internal/agentrunner/lifecycle.go
  - internal/cmd/serve.go
tests:
  - internal/agentrunner/lifecycle_test.go
  - internal/cmd/lifecycle_test.go
decisions:
  - "2026-08-20 : `Lifecycle` est une interface *optionnelle*, jamais fusionnée dans
    `graph.AgentRunner`. `Stub` et `Subprocess` n'ont rien à démarrer — `Subprocess`
    lance son enfant dans `Run`, ce qui est exactement le bon comportement pour un CLI
    one-shot. Élargir le port dont dépend tout le graphe aurait forcé ces deux types
    (et tout futur adaptateur one-shot) à porter des méthodes no-op. L'appelant fait
    une assertion de type ; un test vérifie explicitement que `*Stub` ne satisfait
    *pas* `Lifecycle`, pour que la fuite se voie si elle arrive."
  - "2026-08-20 : un seul `defer p.closeAdapter()` dans `preparedRun.run` couvre les
    trois sorties (succès, échec, stop) — vérifié en traçant les appels avant de le
    supposer. `run` est le point de passage unique des quatre appelants (`run`/`resume`
    en CLI, `StartRun`, `ResumeRun`, `Dispatch`), et `stop` n'est pas un chemin séparé :
    `StopRun` annule le contexte du run via la `steer.Mailbox`, la boucle de niveaux du
    moteur renvoie `ctx.Err()`, et le contrôle revient dans `run` comme pour n'importe
    quel échec. Aucun goroutine ne survit à `run`."
  - "2026-08-20 : `Close()` ne prend pas de contexte, volontairement — le contexte du
    run est précisément ce qui vient d'être annulé sur un stop, et un teardown qui
    refuserait de s'exécuter dans ce cas serait inutile là où il sert le plus."
  - "2026-08-20 : un échec de `Start` avorte le run avant tout nœud (`fmt.Errorf` avec
    `%T` pour nommer l'adaptateur fautif) ; un échec de `Close` est journalisé en
    `slog.Warn` et jamais remonté — même posture best-effort que `reporter.Flush` et
    `checkpoint.Close`. Un run dont tous les nœuds ont réussi a réussi."
  - "2026-08-20 : l'adaptateur est prouvé par un faux (`fakeLifecycleRunner`, test-only,
    enregistre la séquence d'appels) et non par un adaptateur réel : les issues 04/05
    n'existent pas encore. La séquence attendue est vérifiée à l'identique
    (`start, run:…, close`) sur les trois sorties."
---

**Quoi** : `agentrunner.Lifecycle` (`Start(ctx) error` / `Close() error`), interface
optionnelle qu'un adaptateur possédant une ressource à durée de vie supérieure au nœud
(un serveur, un pool, une session) peut déclarer, plus son câblage dans
`preparedRun.startAdapter` / `closeAdapter` appelés une fois par run dans
`internal/cmd/serve.go`.

**Pourquoi** : `Subprocess.Run()` relance un `exec.CommandContext` à *chaque* appel —
correct pour le mode `-p` one-shot de Claude Code, faux pour un adaptateur HTTP
(OpenCode, issue 05) dont le serveur ne doit surtout pas être redémarré à chaque nœud du
graphe. La portée d'un couple `Start`/`Close` est donc le run, jamais le nœud.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts
(14 paquets), `go test -race` vert sur `internal/cmd` et `internal/agentrunner`. Les
trois sorties (succès, échec d'un nœud, stop via `mailbox.Stop()`) sont couvertes par des
tests distincts ; la suppression temporaire du `defer p.closeAdapter()` les fait bien
échouer, donc ils tiennent réellement l'invariant.
