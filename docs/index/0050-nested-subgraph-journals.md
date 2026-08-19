---
id: 0050
feature: nested-subgraph-journals
branch: feature/nested-subgraph-journals
status: done
files:
  - internal/graph/subgraph.go
  - internal/topology/loader.go
  - internal/cmd/runtime.go
  - internal/cmd/serve.go
tests:
  - internal/graph/subgraph_test.go
  - internal/cmd/nested_journal_test.go
decisions:
  - "2026-08-19 : deux fabriques distinctes (`childStep` pour le rapport, `childEvent` pour le journal) auraient pu chacune tirer un id de run frais, mais rien ne les aurait forcées à s'accorder — et `childStep` est appelée en concurrence entre les nœuds sous-graphe d'un même niveau, donc les accorder via une variable partagée aurait demandé un verrou que deux appelants n'ont pas à connaître. `graph.WithChildRun` tire l'id une seule fois par exécution et distribue les deux crochets (`ChildRunHooks{Step, Event}`) ensemble ; `WithChildStep` reste tel quel pour l'appelant qui ne veut que le rapport."
  - "2026-08-19 : le journal d'un enfant n'est pas conditionné par `reporter.Enabled()`. Le journal est la source de vérité de ce dépôt ; le rapport HTTP est un observateur en option. Geler la journalisation d'un enfant sur la présence d'un reporter aurait fait échouer le critère d'acceptation sur `kern-orch run examples/parent.yaml` en configuration par défaut (aucun `KERN_STEP_REPORT_URL`)."
  - "2026-08-19 : le magasin SQLite de l'enfant est ouvert et fermé pour cette seule exécution (`graph.ChildRunHooks.Close`, appelé en `defer` par `SubgraphNode.Execute`), plutôt que mis en cache pour la vie du run parent. Un daemon de longue durée exécute de nombreux runs parents ; une connexion gardée ouverte au-delà de l'unique run imbriqué qu'elle a servi fuirait une connexion par nœud sous-graphe jamais revu."
  - "2026-08-19 : aucun crochet de checkpoint (ligne de projection) n'est câblé pour l'enfant — seulement `OnEvent`. `journalRecorder.record` vide son tampon entier (pas seulement le niveau courant) dès que l'événement terminal du run (`RunFinished`/`RunFailed`) arrive, et `Engine.RunFrom` garantit toujours cet événement. Le journal complet de l'enfant est donc écrit à la fin de son `Run()`, sans avoir besoin d'une ligne de projection intermédiaire par niveau — cette ligne est un cache de lecture (issue 07), pas une condition du rejeu."
  - "2026-08-19 : `nestedRuns` doit maintenant ouvrir son propre magasin, donc reçoit `cfg config.Config` en plus. Son unique site d'appel, `prepareRun` dans `internal/cmd/serve.go`, change d'une ligne (`nestedRuns(reg, reporter, cfg, runID)`) — `cfg` y était déjà en portée. Ce fichier appartient au lot de l'issue 12 (sœur) ; la modification est réduite à ce seul token pour limiter le risque de conflit."
---

**Quoi** : un nœud sous-graphe (`graph.SubgraphNode`) journalise désormais chaque exécution
de son graphe imbriqué sous son propre run id, distinct du run parent, et porte l'événement
`RunStarted` du parent (au niveau où le nœud lui-même produit ses données) — le parent
continue de voir le sous-run comme une seule étape atomique côté checkpoint, exactement comme
avant. `graph.WithChildRun` remplace `WithChildStep` dans le câblage de `cmd.nestedRuns` :
une seule fabrique par exécution retourne à la fois le crochet de rapport HTTP et le crochet
de journalisation, liés au même id de run frais — la même règle que `nestedRuns` appliquait
déjà côté rapport (« un nœud exécuté deux fois est deux runs imbriqués, jamais un run rapporté
deux fois ») s'applique maintenant aussi au journal.

**Pourquoi** : neuvième issue de l'Epic 1 (#13), qui étend au journal une décision déjà prise
côté rapport (`internal/report/http.go`, type `Parent`) : un run imbriqué se rapporte comme un
run à part plutôt que de se fondre dans le flux du parent, parce que le compteur de niveau du
parent est une séquence qu'un puits refuse en écriture désordonnée, et que deux graphes qui
avancent en même temps la corrompraient. L'argument pèse plus lourd pour le journal que pour
le rapport : là, une séquence corrompue coûte une UI confuse ; ici, elle coûte un run
impossible à rejouer.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` (428 tests, 0 échec)
et `go test -race -count=1 ./...` verts sur les 14 paquets. Le cas nommé par les critères
d'acceptation est exercé directement : `TestParentExampleGraphWritesAParentJournalAndAChildJournal`
charge `examples/parent.yaml` → `examples/child.yaml` par `topology.LoadFile`, exécute le
graphe réel, puis vérifie deux id de run distincts en base, une séquence indépendamment
monotone pour chacun, et que `journal/projection.Project` rejoue le même état final (`n = 6`,
3 doublé par l'enfant) sur le journal du parent comme sur celui de l'enfant — la preuve que ce
que le sous-graphe a fusionné dans le parent est bien ce que son propre journal reconstruit
séparément. `TestBuildChildRunHooksMintsAFreshRunIDPerCall` prouve directement, sans passer par
un graphe cyclique, que deux exécutions du même nœud produisent deux id de run distincts.
