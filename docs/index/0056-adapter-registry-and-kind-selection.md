---
id: 0056
feature: adapter-registry-and-kind-selection
branch: feature/adapter-registry-and-kind-selection
status: done
files:
  - internal/config/config.go
  - internal/agentrunner/registry.go
  - internal/cmd/runtime.go
  - internal/cmd/serve.go
tests:
  - internal/config/config_test.go
  - internal/agentrunner/registry_test.go
  - internal/cmd/runtime_agent_test.go
  - internal/cmd/publish_skills_test.go
decisions:
  - "2026-08-20 : `KERN_AGENT_KIND` est une variable à part entière, jamais déduite du
    chemin de `KERN_AGENT_CLI`. Le chemin appartient à l'exploitant (script wrapper,
    shim épinglé, binaire renommé pour un parc) ; le déduire ferait deviner au harnais
    la seule chose qu'il ne peut pas se permettre de rater — le protocole qu'il
    s'apprête à parler."
  - "2026-08-20 : les valeurs reconnues (`claude-code`, `opencode`) sont déclarées dans
    `internal/config` et non dans `internal/agentrunner`, pour que `FromEnv()` rejette
    une valeur inconnue au chargement sans que `config` dépende du paquet qui implémente
    les adaptateurs — la dépendance va dans l'autre sens (CONVENTIONS.md)."
  - "2026-08-20 : `agentrunner.New` est une map de constructeurs plutôt qu'un `switch` :
    ajouter une CLI devient ajouter une entrée, et l'ensemble des kinds supportés est une
    liste lisible au lieu d'une forme de flot de contrôle étalée sur la fonction."
  - "2026-08-20 : les deux branches `claude-code`/`opencode` renvoient une erreur « pas
    encore implémenté » (issues 04 et 05) au lieu de retomber sur `Stub`. Un stub qui
    répond à un run ayant demandé un vrai modèle produit une sortie plausible : un échec
    qui ressemble à un succès, pire ici qu'aucun run du tout."
  - "2026-08-20 : `Subprocess` (le protocole JSON-lines placeholder) devient inatteignable
    depuis le registre — aucun kind ne le sélectionne. Le code est conservé (aucune
    suppression sans accord, CLAUDE.md) mais l'intégration `TestRunBracketsAgentActivity`,
    qui le pilotait via un script shell, a dû être réorientée : elle assied désormais le
    refus au chargement. La couverture bout-en-bout de la chaîne d'activité revient avec
    le premier vrai adaptateur (issue 04)."
---

**Quoi** : `KERN_AGENT_KIND` (`config.EnvAgentKind`, `Config.AgentKind`) sélectionne
l'adaptateur qui parle le protocole de la CLI désignée par `KERN_AGENT_CLI`.
`config.FromEnv()` refuse au chargement un `KERN_AGENT_CLI` sans kind, ou avec un kind
inconnu, en nommant la variable et la valeur fautives. `agentrunner.New(cfg, Options)`
dispatche : pas de CLI configurée → `&Stub{}` (comportement inchangé) ; sinon la map
`adapters`. `internal/cmd`'s `newRunner` renvoie désormais `(graph.AgentRunner, error)`,
propagée par les deux appels de `serve.go`.

**Pourquoi** : issue 02 de l'Epic 2 (#36), fondation des issues 03, 04 et 05. Décisions 01
et 04 du ledger de changement : un registre d'adaptateurs dans `internal/agentrunner`, un
type Go par CLI cible, tous satisfaisant `graph.AgentRunner` inchangé — `graph` et tout
autre paquet restent intacts.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./... -count=1` verts —
468 tests passants, 0 échec, 14 paquets. Chaque test a été vu rouge avant d'être vert
(erreurs de compilation `undefined: EnvAgentKind`, `undefined: New`, `assignment mismatch`
sur `newRunner`), puis vert. Les quatre combinaisons CLI/KIND sont couvertes par des tests
nommés, côté `config` comme côté registre.
