---
id: 0038
feature: activity-narration
branch: feature/activity-narration
status: done
files:
  - internal/report/activity.go
  - internal/agentrunner/subprocess.go
  - internal/cmd/runtime.go
  - internal/cmd/serve.go
  - README.md
tests:
  - internal/report/activity_test.go
  - internal/agentrunner/subprocess_test.go
decisions:
  - "2026-08-11 : message réutilise state[\"display:<nodeID>\"], jamais un second canal de narration — même convention déjà lue par kern-ui pour le panneau détail d'un nœud, décidé pour ne pas inventer une deuxième façon pour un skill de parler de lui-même."
  - "2026-08-11 : additif sur kern.activity/v1 (champ optionnel omitempty), pas de v2 — la fixture contrats/kern.activity.v1.json reste inchangée, un message vide ne voyage jamais sur le fil."
  - "2026-08-11 : seuls les nœuds agent (subprocess) et le nœud d'approbation narrent — les nœuds outil (Go, synchrones) ne sont jamais passés par OnActivity, avant ou après ce changement ; hors scope d'y ajouter cette brique."
---

**Quoi** : `kern.activity/v1` gagne un champ optionnel `message` — narration en langage
clair de ce qu'un nœud vient de faire, portée uniquement par le signal d'arrêt
(`generating: false`). Ni fabriqué, ni écrit par un nouveau canal : `message` est
exactement `state["display:<nodeID>"]` quand le nœud en a écrit un — la même convention
que kern-ui utilise déjà pour le panneau de détail d'un nœud dans la Ruche. Un nœud qui
n'écrit pas cette clé ne narre rien ; c'est un état normal et silencieux, pas une erreur.

**Pourquoi** : C14 côté Kern-UI — le panneau « Actions récentes de l'agent » du mockup
`avel-admin.dc.html` n'avait aucune source réelle. Décidé avec l'utilisateur
(AskUserQuestion, 2026-08-11) de construire la version « narration dynamique par skill »
plutôt que la version minimale (accumulation des signaux existants + libellés statiques
déjà en place côté kern-ui) — plus fidèle au mockup, au prix d'un changement cross-repo.

**Comment** : `agentrunner.Subprocess.Run` capture `graph.AgentResult` avant le `defer` qui
ferme le crochet `OnActivity` (au lieu de fermer sur des arguments figés à l'ouverture), et
lit `result.Output["display:"+nodeID]` pour construire le message du signal d'arrêt.
`activityRelay` (le pont entre le runner et `report.ActivityReporter`, `internal/cmd/
runtime.go`) et `wireApproval` (qui bracket l'attente d'une décision humaine) propagent le
paramètre — vide pour une approbation, il n'y a rien à narrer sur une attente.

**Vérifié en réel** : `go test ./internal/report/... ./internal/agentrunner/...` verts
(nouveaux tests : le message voyage sur le fil quand présent, absent — jamais une chaîne
vide — quand il n'y en a pas ; `Subprocess` narre exactement `display:<nodeID>` sur l'arrêt
et rien quand le nœud n'en a pas écrit). `gofmt -l` propre sur tous les fichiers touchés.

**Piège d'environnement, pas de ce changement** : `go build ./...` échoue sur ce poste
avant même ce travail (`git stash` + build confirme que `dev` lui-même ne compile pas
ici) — `Kern-Anon`/`PresidioGo` a été renommé en `github.com/kern-ia/kern-anon` (la
migration d'organisation GitHub vue ce même jour côté Kern-UI, message « This repository
moved » au push) mais le `require`/`replace` de `go.mod` ici pointe encore l'ancien module
`github.com/YoLaub/PresidioGo`. Non corrigé ici — hors scope de C14, à traiter à part.
`internal/report` et `internal/agentrunner` ne dépendent pas de cette chaîne et compilent/
testent proprement en isolant les packages (`go build ./internal/report/... ./internal/
agentrunner/...`) ; `internal/cmd` n'a pu être vérifié que par lecture + `gofmt`, pas par un
build réel sur ce poste — à confirmer une fois le module renommé corrigé.
