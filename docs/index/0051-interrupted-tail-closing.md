---
id: 0051
feature: interrupted-tail-closing
branch: feature/interrupted-tail-closing
status: done
files:
  - internal/checkpoint/interrupted.go
  - internal/checkpoint/resume_point.go
  - internal/journal/event.go
  - internal/journal/projection/projection.go
tests:
  - internal/checkpoint/interrupted_test.go
  - internal/journal/event_test.go
  - internal/journal/projection/replay_test.go
  - internal/cmd/replay_resume_test.go
decisions:
  - "2026-08-19 : le discriminant d'une interruption est **l'absence d'événement terminal**, pas la forme de la queue. Deux accidents la produisent : un processus tué en plein niveau (un `LevelOpened` sans `LevelClosed`, des nœuds démarrés jamais résolus) et un run stoppé par son propre contexte — ses événements terminaux passent par ce contexte déjà annulé, donc l'écriture est refusée et le journal s'arrête net après le dernier niveau clos (constat de l'issue 08). **Les deux sont fermés.** Le second a l'air propre, et c'est précisément pourquoi il doit l'être aussi : « le record s'arrête » et « le run s'est terminé » ne sont pas le même fait, et rien d'autre dans la table ne les distingue. Un run réellement terminé ne gagne rien : un journal fini sur `run_finished`, `run_failed` ou `run_interrupted` n'est pas touché, aucune ligne réécrite, aucun événement ajouté."
  - "2026-08-19 : **aucun `LevelClosed` synthétique.** Fermer le niveau ouvert ferait replier ses branches partielles dans l'état partagé par le rejeu, or le moteur abandonne un niveau en bloc — l'état obtenu ne serait un état qu'aucun run n'a jamais eu, exactement ce que le critère d'acceptation interdit. Le niveau reste ouvert **à dessein** : c'est lui qui dit à la reprise quel niveau redémarrer, et il ne contribue à rien tant qu'il n'est pas clos pour de vrai. Sont écrits : un `NodeFailed` par nœud démarré et jamais résolu (un `NodeStarted` que rien ne répond est l'ambiguïté que l'issue existe pour supprimer), puis un `RunInterrupted`. Un nœud qui avait déjà produit est laissé tel quel — lui inventer un échec serait un mensonge, et sa sortie est inoffensive puisque le niveau n'a jamais combiné."
  - "2026-08-19 : le marquage synthétique est un champ `Synthetic bool` sur l'**enveloppe** `journal.Event`, pas un champ dans chaque payload reconstructible ni un second `Kind`. C'est de la provenance, pas du contenu : un `RunInterrupted` synthétique dit du run la même chose qu'un vrai, simplement personne n'était vivant pour le dire. `omitempty` : un événement observé s'encode exactement aux octets qu'il produisait avant l'existence du champ, ce qui rend vérifiable octet par octet qu'un journal cohérent n'a pas été réécrit."
  - "2026-08-19 : la fermeture vit dans `ResumePoint`, qui écrit donc malgré son nom. Alternative écartée : une méthode exportée que les deux points d'entrée de reprise appellent chacun — ils passent par `ResumePoint` justement pour que la CLI et le daemon ne divergent pas, et une étape que l'un des deux peut oublier est une étape que l'un des deux oubliera. Écriture via `AppendAndReproject` : le lot subit le même contrat de séquence que tout autre append, et la ligne reste `Project(journal)` puisqu'elle est redérivée dans la transaction qui écrit ces événements. Effet de bord utile : si le run est en fait encore vivant, son propre écrivain a déjà avancé la séquence et l'append synthétique est refusé au lieu de s'y intercaler."
  - "2026-08-19 : `projection` gagne une règle — un second `RunStarted` **rouvre** le record et jette le niveau non clos de la tentative précédente. Sans elle, le premier niveau de tout run repris échouerait au rejeu, son `RunStarted` arrivant désormais après un événement terminal. Seul `RunStarted` rouvre : tout autre événement après une fermeture reste refusé, sinon un `LevelClosed` perdu deviendrait indiscernable d'un run réellement repris."
---

**Quoi** : un run dont le journal ne porte aucun événement terminal se voit, au moment de la
reprise, adjoindre des événements **synthétiques** — les échecs des nœuds restés en l'air, puis
l'interruption du run — marqués comme tels sur l'enveloppe. La reprise dérive ensuite sa
frontière du journal ainsi fermé. Un journal déjà terminé n'est pas touché.

**Pourquoi** : douzième issue de l'Epic 1 (#23), décision 05, détachée de l'issue 08 pour la
taille. Le trou était invisible dans l'ancien design (aucun checkpoint n'était écrit, la
reprise relançait simplement le niveau) ; avec des événements par nœud il est visible, et le
laisser implicite ferait se confondre dans un même journal deux tentatives sans frontière.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets (434 tests). Deux mutations confirment
que les tests portent : retirer `Synthetic: true` de la queue fait échouer
`TestTheEventsThatCloseAnInterruptedTailAreMarkedSynthetic` **et** le test bout en bout
`TestResumingAStoppedRunRecordsTheInterruptionInItsJournal`, qui part d'un vrai run stoppé en
vol par le daemon ; et avant la règle de réouverture du rejeu, ce même run repris échouait au
premier niveau (« arrives after the run was already closed by run_interrupted »). L'invariance
d'un journal cohérent est vérifiée sur les **octets stockés** — les blobs de la table `events`
relus avant et après une reprise, pas les événements décodés, qu'un aller-retour d'encodage
rendrait identiques à tort.
