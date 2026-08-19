---
id: 0049
feature: replay-based-resume
branch: feature/replay-based-resume
status: done
files:
  - internal/journal/projection/replay.go
  - internal/journal/projection/projection.go
  - internal/checkpoint/resume_point.go
  - internal/cmd/commands.go
  - internal/cmd/serve.go
tests:
  - internal/journal/projection/replay_test.go
  - internal/checkpoint/resume_point_test.go
  - internal/cmd/replay_resume_test.go
decisions:
  - "2026-08-19 : `checkpoint.ResumePoint` est `Record` moins l'état de la ligne, exactement comme `checkpoint.Projection` côté écriture. La garantie n'est pas une discipline d'appelant mais une absence de champ : le chemin de reprise n'a plus par où lire le cache, même par accident. La ligne reste lue pour la provenance que le journal ne porte pas — le chemin du graphe surtout, qui est ce qui dispense `resume <run-id>` d'un argument."
  - "2026-08-19 : la frontière vient du journal quand le journal en nomme une — un niveau ouvert et jamais clos est le niveau à rejouer, et aucune ligne n'est jamais écrite pour lui. Quand tous les niveaux sont clos, le journal n'en nomme aucune : le moteur calcule la frontière suivante à partir des routes des branches qu'il vient de combiner, et le résultat d'une route n'est pas un événement. Réévaluer les routes ici a été écarté — une route s'applique à la branche de son nœud, et après un fan-out fusionné ces branches n'existent plus, donc la réponse ne coïnciderait que pour les niveaux à un nœud. La ligne fournit alors cette frontière, comme une position et non comme un état ; un journal qui enregistre le run comme terminé prime sur elle."
  - "2026-08-19 : un run doté d'une ligne mais d'aucun événement — checkpointé avant que le journal existe — est **refusé** (`checkpoint.ErrNoJournal`, message nommant le run) et non repris. Le rejouer projette un état vide, qui n'est pas un état que ce run a eu mais l'absence de trace : reprendre silencieusement là-dessus ferait lire du vide à tous les nœuds restants, sans une erreur nulle part. C'est le trou signalé par l'auteur de l'issue 07 ; refuser fort suit la règle « fail loud » de CONVENTIONS.md."
  - "2026-08-19 : `projection.Replay` remplace `Project` comme parcours unique (`Project` en devient un mince appel). L'état et la position sortent du même passage, donc un appelant ne peut pas prendre l'état d'une lecture du journal et la frontière d'une autre."
  - "2026-08-19 : fermer une queue interrompue n'est **pas** ici — issue 12 (#23). Cette issue suppose un journal cohérent. Constat annexe : un run stoppé n'écrit pas ses événements terminaux, parce que l'écriture passe par le contexte du run, déjà annulé ; son journal s'arrête donc après le dernier niveau clos. Ce cas retombe sur la frontière de la ligne et se reprend correctement, mais rendre cette interruption explicite reste l'issue 12."
---

**Quoi** : `resume` ne lit plus l'état de la ligne `checkpoints`. `checkpoint.ResumePoint`
rejoue les événements du run, en projette l'état, en déduit la frontière à exécuter, et n'y
adjoint de la ligne que la provenance (chemin du graphe, demandeur, dossier).
`preparedRun.run` prend ce point de reprise au lieu d'un `Record`, si bien que la CLI
(`resume <run-id>`) et `POST /api/v1/runs/{id}/resume` sont la même opération.

**Pourquoi** : huitième issue de l'Epic 1 (#12), décision 05. Sans rejeu, rien dans le système
ne prouve jamais que le journal est complet, et un journal incomplet jamais exercé est pire
que pas de journal : il a l'air de faire autorité. La reprise est le seul chemin où se tromper
est irrattrapable, puisque le run continue depuis un état qui n'a jamais existé.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets. Deux mutations confirment que les
tests portent : renvoyer `rec.State` au lieu de l'état rejoué fait échouer
`TestCorruptingTheCachedRowDoesNotChangeWhatResumeReconstructs` sur `n = 999` au lieu de 3 ; et
avant le câblage, le test bout en bout `TestResumeIgnoresACorruptedRowAndReplaysTheJournal`
échouait en montrant un niveau réellement exécuté avec la valeur corrompue. Ce test passe par
le daemon — donc par l'appel que fait la route HTTP de reprise : run réel arrêté en vol, ligne
corrompue à la main, reprise, approbation, et l'état vivant que le rapporteur publie après la
reprise porte la valeur du journal.
