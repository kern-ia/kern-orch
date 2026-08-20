---
id: 0059
feature: opencode-adapter
branch: feature/opencode-adapter
status: done
files:
  - internal/agentrunner/opencode.go
  - internal/agentrunner/opencode_wire.go
  - internal/agentrunner/registry.go
tests:
  - internal/agentrunner/opencode_test.go
decisions:
  - "2026-08-20 : `Start` attend une *vraie* disponibilité, jamais un `sleep`. La sonde
    est `GET /session` — le sous-système que `Run` utilise — et non une route de santé
    dédiée : une route `health` peut répondre alors que le magasin de sessions démarre
    encore, et un « healthy » qui n'implique pas « peut servir l'unique appel de cet
    adaptateur » est exactement la fausse disponibilité que cette attente existe pour
    écarter. Prouvé par un faux serveur *compilé* (binaire Go bâti dans le test) qui
    dort 900 ms avant d'écouter : un `sleep` fixe passerait ou échouerait au hasard
    face à lui."
  - "2026-08-20 : `Close` atteint la quiescence : SIGTERM, attente du `cmd.Wait()`
    réel, `Kill` seulement si le délai de grâce expire, puis nouvelle attente
    inconditionnelle. Un `Close` qui rendrait la main sur le signal seul laisserait le
    port occupé après la fin annoncée du run, et le `Start` du run suivant échouerait
    sur un port qui, de tout point de vue visible, devrait être libre. Le test
    vérifie `syscall.Kill(pid, 0) == ESRCH`, pas que le signal a été envoyé."
  - "2026-08-20 : `exec.Command` et non `exec.CommandContext`. Lier le serveur au
    contexte du run ferait tuer l'enfant sous les pieds de `Close` sur un stop —
    précisément la chose que son attente existe pour faire."
  - "2026-08-20 : le flux réel est create-session → `POST /session/:id/message` →
    `GET /session/:id/message`, et non le seul `POST` du texte de l'issue. Le corps de
    réponse du `POST` ne contient que le *dernier* message de la session : le message
    porteur de l'appel d'outil disparaîtrait, donc les outils qu'un nœud a réellement
    exécutés n'atteindraient jamais l'état. C'est le flux que l'issue 01 a vérifié en
    réel (`testdata/README.md`)."
  - "2026-08-20 : une session par appel de `Run`, pas une session partagée par run.
    Les sessions OpenCode accumulent l'historique : une session partagée donnerait à
    chaque nœud les tours des nœuds précédents, alors que l'état du graphe est le canal
    de contexte explicite. Un second canal implicite derrière lui ferait dépendre
    l'entrée d'un nœud de l'ordre d'exécution de ses frères. C'est aussi la forme dont
    le fan-out concurrent (issue 06) a besoin."
  - "2026-08-20 : les quatre clés d'état sont suffixées par l'ID du nœud
    (`display:<id>`, `opencode:session:<id>`, `opencode:tools:<id>`,
    `opencode:finish:<id>`). Deux nœuds agents d'un même niveau écrivent dans un état
    partagé : une clé plate ferait écraser silencieusement la provenance d'une branche
    par sa sœur. La réponse va sous `display:<nodeID>` parce que l'adaptateur ignore ce
    que le nœud devait produire, et que c'est le seul nom qu'un consommateur et
    `OnActivity` lisent déjà. Écartés : les parts `reasoning` (le modèle qui pense tout
    haut, qui serait recopié dans chaque prompt aval via l'état persisté) et
    `cost`/`tokens` (réels, mais aucun sink ne les demande)."
  - "2026-08-20 : `TokenSink` reçoit la réponse d'un seul coup, pas en incrémental.
    `POST /session/:id/message` est synchrone et l'audit de l'épique n'a trouvé aucune
    primitive de streaming documentée pour ce chemin (les flux `/event` sont hors
    périmètre). Le contrat « tout ce que le modèle a dit atteint le sink » tient ; le
    contrat « au fil de la production » ne tient pas, et n'est pas revendiqué.
    `OnActivity` encadre bien `true`/`false` autour de l'aller-retour complet, sur
    toutes les sorties y compris les erreurs."
  - "2026-08-20 : le champ du nom d'outil est lu sous *deux* orthographes. La capture
    réelle (opencode 1.18.19) envoie `\"tool\":\"bash\"` ; le document OpenAPI servi par
    ce même serveur nomme ce champ `name` dans `SessionMessageAssistantTool`. Lire les
    deux, c'est suivre ce que le serveur envoie plutôt que parier sur l'actualité du
    document."
---

**Quoi** : `agentrunner.OpenCode`, adaptateur HTTP pour le CLI OpenCode — il implémente
`graph.AgentRunner` *et* `agentrunner.Lifecycle`, possède le processus `opencode serve`
pour la durée d'un run, et traduit le transcript `{ info, parts }` réel en
`graph.AgentResult`. Câblé dans le registre de l'issue 02 à la place du placeholder
`"opencode"`.

**Pourquoi** : OpenCode n'a aucun protocole stdin/stdout — c'est un serveur HTTP
long-vivant. C'est le premier usage réel du port `Lifecycle` (fiche 0057), qui existe
justement parce que relancer ce serveur à chaque nœud paierait plusieurs secondes de
démarrage par nœud du graphe.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts
(14 paquets), `go test -race` vert sur `internal/agentrunner` et `internal/cmd`. Le
tour complet contre le **vrai binaire** `opencode` 1.18.19 passe (spawn → une question →
un résultat → arrêt propre, processus prouvé disparu), et se skippe seul quand le binaire
est absent. Le test de disponibilité utilise un serveur factice **compilé** qui dort
avant d'écouter, parce qu'un faux en-processus ne peut pas exercer ce qui est en jeu : un
enfant qui n'accepte pas encore de requête au retour d'`exec.Start`.
