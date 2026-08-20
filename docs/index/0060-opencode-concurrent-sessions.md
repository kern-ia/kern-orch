---
id: 0060
feature: opencode-concurrent-sessions
branch: feature/opencode-concurrent-sessions
status: done
files:
  - internal/agentrunner/opencode.go
tests:
  - internal/agentrunner/opencode_concurrency_test.go
decisions:
  - "2026-08-20 : aucune ligne de production n'a changé. La lecture de `Run` (issue
    05) confirme que chaque appel ouvre sa propre session (`createSession` avec le
    `NodeID` de l'appel), et que le seul état partagé entre appels — `baseURL` et
    `*http.Client` — est en lecture seule après `Start` : `http.Client` est
    sûr pour un usage concurrent par sa propre doc. Rien ne verrouille `Run` ; la
    forme concurrente promise par `graph.Engine.runLevel` (un goroutine par nœud du
    front) traverse donc l'adaptateur sans y être défaite."
  - "2026-08-20 : la preuve de non-sérialisation est structurelle, pas temporelle. Le
    faux serveur bloque la *première* requête `POST /session` reçue jusqu'à ce
    qu'une *seconde*, distincte, arrive physiquement — si `Run` sérialisait les
    appelants derrière un verrou, cette seconde requête n'existerait jamais tant que
    la première tient le verrou, et le test échouerait par timeout (5 s) plutôt que
    par une fenêtre de chance. Vérifié à l'envers : un verrou `sync.Mutex` temporaire
    ajouté à `Run` fait échouer le test avec exactement ce message, puis retiré."
  - "2026-08-20 : `go test -race` est vert sur tout le module (14 paquets), y compris
    ce nouveau test — le détecteur de races ne voit rien à signaler sur les accès
    concurrents à `*OpenCode`."
  - "2026-08-20 : le fan-out réel contre `opencode serve` (binaire 1.18.19 présent sur
    cette machine) est passé sans surprise : trois nœuds agents sous une seule
    instance `OpenCode`, chacun avec sa propre session et sa propre réponse
    indépendante dans l'état fusionné. Aucune limite de concurrence par instance
    n'est apparue — le serveur a servi les trois sessions sans throttling visible."
  - "2026-08-20 : aucun changement à `graph.Engine.runLevel` — hors périmètre par
    construction (l'issue ne fait que prouver que l'adaptateur compose correctement
    avec une garantie qui existe déjà)."
---

**Quoi** : un test de non-sérialisation structurel
(`TestConcurrentRunCallsOpenDistinctSessionsWithoutSerializing`) et un test de fan-out
réel (`TestRealOpenCodeFanOutGraphProducesIndependentResultsPerNode`) pour
`agentrunner.OpenCode`. Aucun code de production modifié — l'issue prouve que la forme
« une session par `Run()` » de l'issue 05 tient déjà sous charge concurrente.

**Pourquoi** : `graph.Engine.runLevel` lance un goroutine par nœud du front et appelle
`AgentRunner.Run` sur la **même** instance d'adaptateur pour tous. `Subprocess` tenait
cette garantie par accident (un process par appel, aucun état partagé) ; `OpenCode`
possède un unique serveur HTTP persistant pour tout le run, donc rien ne garantissait
a priori qu'une implémentation future n'y ajoute pas un verrou qui déferait
silencieusement le parallélisme promis partout ailleurs par le moteur.

**Vérifié en réel** : `go build ./...`, `go vet ./...` verts. `go test -race ./...`
vert (14 paquets). Le test structurel a été vérifié à l'envers : un verrou temporaire
ajouté à `Run` le fait échouer avec le message attendu, retiré ensuite. Le fan-out réel
contre le vrai binaire `opencode` 1.18.19 passe et se skippe seul quand le binaire est
absent, comme le test de round-trip de l'issue 05.
