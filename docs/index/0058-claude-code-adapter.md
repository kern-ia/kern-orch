---
id: 0058
feature: claude-code-adapter
branch: feature/claude-code-adapter
status: done
files:
  - internal/agentrunner/claudecode.go
  - internal/agentrunner/adapter.go
  - internal/agentrunner/registry.go
tests:
  - internal/agentrunner/claudecode_test.go
  - internal/agentrunner/claudecode_e2e_test.go
  - internal/agentrunner/integration_test.go
decisions:
  - "2026-08-20 : `--verbose` fait partie des flags de protocole, pas du confort. Vérifié
    en réel contre `claude` 2.1.237 : `claude -p --input-format=stream-json
    --output-format=stream-json` sans `--verbose` refuse de démarrer (« When using
    --print, --output-format=stream-json requires --verbose »). Les trois flags
    documentés seuls ne produisent donc *pas* une invocation qui marche — c'est
    exactement le genre de détail qu'une implémentation écrite d'après la doc rate."
  - "2026-08-20 : la forme d'entrée n'est pas devinée. Le README de `testdata/` (issue 01)
    documente l'objet exact qui a été *piped* sur stdin pour produire la capture commitée :
    `{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":…}]}}`.
    Aucune capture d'entrée supplémentaire n'a été nécessaire, et le round trip réel le
    confirme. Le protocole n'a aucun champ pour l'état du run : l'état voyage donc dans le
    bloc texte, sous un intitulé — le prompt est le seul canal offert, et le cacher dans un
    fichier annexe rendrait le contexte d'un nœud invisible dans la transcription du CLI."
  - "2026-08-20 : mapping wire → sortie du nœud, décidé explicitement. `result.result` →
    `Output[\"display:<nodeID>\"]` (convention déjà en place dans le repo pour « la sortie
    d'un nœud en clair »), donc la réponse finale est à la fois ce que lit un nœud aval et ce
    que `OnActivity` narre à l'arrêt. `result.session_id` →
    `Output[\"claude-code:session:<nodeID>\"]`, provenance permettant de rapprocher un run de
    la transcription de Claude Code. Les blocs `text` des messages `assistant` vont dans
    `TokenSink` ; les blocs `tool_use` et les messages `user` porteurs de `tool_result` en
    sont exclus : le sink est le flux de réponse, pas une trace du travail interne du CLI.
    Le dernier message `result` gagne."
  - "2026-08-20 : granularité honnête du « streaming ». La capture d'issue 01 ne contient
    aucun delta partiel (son README le note comme lacune assumée : `--include-partial-messages`
    n'a pas été exercé). `TokenSink` reçoit donc un message complet à la fois, pas des
    tokens. Le contrat `io.Writer` ne change pas si l'option est activée plus tard."
  - "2026-08-20 : `Subprocess` et `protocol.go` sont **supprimés**, pas conservés derrière un
    troisième `KERN_AGENT_KIND`. Depuis l'issue 02, `config.FromEnv` refuse un
    `KERN_AGENT_CLI` sans `KERN_AGENT_KIND` : plus aucun chemin configuré ne construit un
    `Subprocess`, et `NewSubprocessFromEnv`/`EnvCLIPath` n'avaient plus un seul appelant. Le
    code était déjà injoignable, pas seulement redondant. Le mandat de suppression vient du
    périmètre accepté de l'épique (remplacer ce placeholder est sa raison d'être), ce qui
    satisfait la règle « ne jamais supprimer sans consentement » de CONVENTIONS.md."
  - "2026-08-20 : `Start`/`Close` no-op déclarés quand même. Un appel = un processus frais,
    il n'y a rien à démarrer ; mais déclarer la paire évite que le site d'appel ait à savoir
    quels adaptateurs ont opté pour `Lifecycle`. Le commentaire de `lifecycle.go` a été
    corrigé en conséquence : c'est `Stub` qui prouve encore que l'interface est optionnelle."
---

**Quoi** : `agentrunner.ClaudeCode`, l'adaptateur `graph.AgentRunner` qui parle le vrai
protocole `claude -p --input-format=stream-json --output-format=stream-json --verbose` :
un processus par appel, un message `user` stream-json sur stdin, les messages typés du CLI
lus ligne par ligne sur stdout. Câblé dans le registre sur `KERN_AGENT_KIND=claude-code`.

**Pourquoi** : le protocole JSON-lines précédent (`protocol.go`) était inventé — un
`Request{node_id,prompt,state}` et des événements `token`/`result`/`error` qu'aucun CLI
réel n'émet. Toute l'épique 2 existe pour remplacer cette forme supposée par celle que le
binaire accepte réellement.

**Vérifié en réel** : `go build ./...`, `go vet ./...`, `go test ./...` verts (14 paquets).
Le test de traduction décode le fixture verbatim de l'issue 01 — les deux captures (tour
texte simple et aller-retour d'outil) sont assertées séparément, filtrées par `session_id`,
sans jamais éditer le fichier. Round trip réel contre `claude` 2.1.237 vert en 6,86 s
(`KERN_AGENT_CLI=$(command -v claude) KERN_AGENT_KIND=claude-code go test
./internal/agentrunner/ -run TestClaudeCodeRoundTripsAgainstTheRealCLI`), self-skip sinon.
