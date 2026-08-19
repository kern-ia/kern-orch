---
id: 0054
feature: runtime-equivalence-check
branch: feature/runtime-equivalence-check
status: done
files:
  - internal/config/config.go
  - internal/cmd/equivalence_check.go
  - internal/cmd/serve.go
  - internal/cmd/commands.go
tests:
  - internal/config/config_test.go
  - internal/cmd/equivalence_check_test.go
decisions:
  - "2026-08-19 : FromEnv() change de signature — `Config` seul devient `(Config, error)` — pour que l'issue 13 puisse échouer bruyamment au chargement plutôt que retomber en silence sur la valeur par défaut d'EnvRuntimeEquivalenceCheck (règle CONVENTIONS.md « misconfiguration fails loud »). Les cinq appelants réels (dans les RunE de commands.go et serve.go) propagent l'erreur ; le seul appel hors RunE (la valeur par défaut du flag --skills-dir de publish-skills, construite avant tout parsing) avale l'erreur avec un commentaire l'expliquant, puisque ce seul appel ne lit que SkillsDir et que le RunE de la même commande rappelle FromEnv et échoue bruyamment sur le vrai chemin d'exécution."
  - "2026-08-19 : le check est câblé uniquement dans preparedRun.run (serve.go), partagé par run/resume/le daemon — pas dans buildChildRunHooks (les runs imbriqués). Câbler les deux aurait dépassé le S visé sans que l'issue ou le fichier d'issue ne l'exige explicitement ; noté comme piste de suivi plutôt que fait silencieusement."
  - "2026-08-19 : la comparaison elle-même n'est pas réécrite — equivalenceCheckHook rejoue exactement ce qu'assertReplayEquivalent (issue 10, replay_equivalence_test.go) fait déjà : encoder les deux états en JSON via graph.State.MarshalJSON et comparer les octets. La seule addition est divergentStateKeys, qui décode les deux encodages dans une vue générique (stateWireView) pour nommer les clés qui diffèrent au lieu de seulement faire échouer un test — nécessaire au runtime, qui n'a pas de sortie t.Fatalf à lire."
  - "2026-08-19 : « pas de travail de projection supplémentaire par niveau » quand le check est désactivé est garanti structurellement, pas par un branchement interne à un hook toujours présent — equivalenceCheckHook renvoie nil quand enabled est faux, et multiStep saute déjà un hook nil. Prouvé par deux tests jumeaux qui font tourner exactement la même chaîne multiStep(checkpointHook, equivalenceCheckHook) et comptent les appels à un point d'indirection (equivalenceProject) substitué en test : zéro appel désactivé, un appel activé pour un run à un seul niveau — voir TestDisabledEquivalenceCheckCallsProjectionZeroTimesPerLevel et son jumeau."
---

**Quoi** : un champ `Config.RuntimeEquivalenceCheck` (env `KERN_RUNTIME_EQUIVALENCE_CHECK`),
off par défaut, qui active à chaque frontière de niveau une comparaison entre la projection du
journal que `checkpointHook` vient d'écrire et l'état vivant que le moteur porte — la même
comparaison que l'issue 10 a prouvée juste sur des runs réels, exposée ici pour un run en
production plutôt que pour un test. Activé, une divergence fait échouer le run bruyamment en
nommant les clés qui divergent (`data.<clé>`, `step`, `frozen`, `zones.<clé>`). Désactivé, le
hook n'existe même pas dans la chaîne : aucun appel de projection supplémentaire par niveau.

**Pourquoi** : douzième et dernière issue de l'Epic 1 (#24), scindée de l'issue 10 qui
dépassait la taille d'une PR revue. L'issue 10 prouve que l'invariant tient ; celle-ci le rend
diagnosticable sur un run suspect sans reconstruire le binaire.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` et
`go test -race -count=1 ./...` verts sur les 14 paquets. La détection de corruption et le
nommage des clés divergentes sont prouvés en faisant tourner un run réel à travers l'engine
puis en présentant au hook un état vivant délibérément altéré (voir
`TestEquivalenceCheckHookDetectsACorruptedProjectionAndNamesTheDivergentKey`) — vérifié en
échec réel en désactivant temporairement le nommage et en observant le test échouer avant de
restaurer le code. Le « zéro appel supplémentaire » est vérifié de la même façon : un test
modifié pour appeler la projection même désactivé fait échouer
`TestDisabledEquivalenceCheckCallsProjectionZeroTimesPerLevel`, confirmant que le test détecte
une vraie régression et pas seulement une tautologie.
