# Rétro — Kern-Orch

Journal des pièges spécifiques au projet, ajoutés au moment où ils mordent.
Les pièges génériques (réutilisables hors projet) remontent aussi dans le skill
`greenfield-tdd-okf/references/pieges.md`.

## 2026-07-20 — Bootstrap
- `agentrunner` dépend d'une brique CLI externe non accessible (GitHub d'un collègue).
  Décision : `AgentRunner` interface + `stubRunner` déterministe → l'app tourne sans
  aucune config LLM ; le vrai `subprocessRunner` se branche via `KERN_AGENT_CLI`.
  (applique le piège générique « mode simulé si secret/service externe requis »)

## 2026-07-20 — Agentrunner
- Typed-nil Go : un `*bytes.Buffer` nil affecté à un champ `io.Writer` donne une interface
  NON nil → le garde `if w == nil` rate et `io.WriteString` panique (nil deref). Ne jamais
  stocker un pointeur typé nil dans un champ interface ; ne l'assigner que s'il est non-nil.
  (générique → remonté dans le skill pieges.md)
- E2E subprocess testé avec un vrai process externe (script sh dans t.TempDir()) en plus du
  pattern TestHelperProcess — prouve le chemin réel Engine→AgentNode→CLI sans la brique du collègue.

## 2026-07-20 — Clôture v0.1.0
- Squelette complet livré : graph engine, agentrunner (stub+subprocess), checkpoint+resume,
  skills registry, topology loader YAML, config, CLI (run/resume/status/list-skills). Tout vert
  sous `go test -race`, E2E prouvé au binaire (stub ET vraie CLI subprocess).
- Inversion de dépendance tenue : `graph` définit les ports (AgentRunner, StepFunc) ; les
  packages d'infra (agentrunner, checkpoint) dépendent de `graph`, jamais l'inverse.
- Dettes assumées à réconcilier plus tard :
  1. Contrat JSON-lines agentrunner PROVISOIRE (§6.4) — à aligner sur la vraie CLI du collègue.
  2. resume redemande le chemin YAML (non persisté dans le checkpoint) — envisager de le stocker.
  3. builtinRegistry ne fournit que `noop` — les projets enregistrent leurs tools/routers en Go.
  4. modernc.org/sqlite : ouvrir avec PRAGMA busy_timeout/WAL si accès concurrent multi-run un jour.

## 2026-07-20 — Sous-graphes (v0.2.0)
- Dernier concept LangGraph manquant (§3) livré : SubgraphNode + `type: subgraph` YAML via
  topology.LoadFile (résolution fichier récursive + garde anti-cycle). Portage §3 désormais complet.
- Garde récursion : set "in-progress" par chemin absolu, supprimé au retour → cycle a→b→a détecté,
  réutilisation DAG d'un même fichier autorisée.
- Reste ouvert et inchangé : contrat §6.4 (CLI collègue), resume redemande le YAML.

## 2026-07-22 — Clôture EPIC-01 (resume+graphpath, zones/gel)
- Checkpoint : chemin du graphe persisté (colonne `graph_path DEFAULT ''`) → `resume <run-id>`
  sans re-fournir le YAML. Toujours stocker le chemin ABSOLU (résolu au run) sinon resume
  depuis un autre cwd casse.
- Zones de contexte + gel : le `Merge` du moteur est **additif** (n'exprime pas la suppression).
  Un `Freeze` (respawn contexte frais) sur un clone puis merge ne propageait pas les
  suppressions ni le compteur `Frozen`. Fix : frontière **mono-nœud → remplacement** par la
  branche (elle contient déjà tout le state) ; **fan-out (>1) → merge additif**. Piège de
  moteur graphe à retenir : un merge additif ne peut pas modéliser un « reset/gel » de contexte.

## 2026-07-26 — step-reporter

**A fonctionné**
- Traiter le reporter comme de l'infra (`internal/report`) et non comme du câblage dans
  `cmd` : la direction de dépendance reste celle du reste du repo, report → graph.
- Tester d'abord les modes de panne (sink 500, injoignable, URL invalide, hôte inexistant)
  plutôt que le chemin heureux : c'est ce qui a figé la règle « le hook retourne toujours nil ».
- Vérifier au binaire contre un vrai kern-ui, pas seulement contre un httptest.

**À surveiller**
- `Engine.OnStep` n'a qu'un seul slot. Tant qu'il reste ainsi, tout nouvel observateur doit
  passer par `multiStep` côté appelant. Si les observateurs se multiplient, c'est le signal
  qu'`OnStep` devrait accepter une liste — mais c'est une modification de `graph`, à peser.
- Le reporter est synchrone : un sink lent ralentit le graphe, borné par un timeout de 2 s
  par niveau. Acceptable en local ; à revoir si le sink devient distant.
- Le state traverse aplati. Si un jour les zones ou le compteur de gel intéressent un
  observateur, c'est un ajout explicite au contrat, pas une sérialisation du State.

## 2026-07-26 — topologie et échec

**Ce qui a été découvert en implémentant**
- Les arêtes du `Graph` runtime sont des `RouteFunc`. On ne peut donc pas exporter la
  topologie depuis le moteur : il a fallu la relire du YAML. Une route conditionnelle reste
  inconnaissable avant l'exécution, d'où le drapeau `dynamic` plutôt qu'une arête inventée.
- Le hook `StepFunc` ne voit jamais un échec — le moteur le signale en retournant de `Run`.
  Sans un appel dédié, un run cassé était indiscernable d'un run terminé côté sink.
- Premier jet de `ReportFailure` : frontière vide. Résultat, le consommateur savait qu'un run
  avait échoué mais pas où. La frontière active au moment de la casse est l'information utile.

**À surveiller**
- `stepCounter` mémorise niveau et frontière uniquement pour pouvoir rapporter l'échec. Si le
  moteur exposait un jour l'erreur au hook, ce bricolage disparaîtrait.

## 2026-07-27 — registry-contract

**A fonctionné**
- `RegistryPublisher` séparé de `HTTPReporter` plutôt qu'une méthode de plus : deux
  endpoints, deux URL, deux responsabilités. `postJSON` extrait ensuite pour que la
  plomberie HTTP reste unique.
- Le test qui vérifie qu'aucun champ ne transporte le répertoire du skill
  (`TestPublisherDoesNotLeakTheSkillDirectory`) : il interdit la fuite par n'importe quel
  nom de champ, pas seulement par `dir`.

**À surveiller**
- `DeclaredNode` jetait la référence `skill:` que `nodeSpec` lisait déjà. Un champ parsé
  puis abandonné en cours de route est un contrat qui manque en aval sans que rien ne le
  signale.
- `list-skills` code en dur `"skills"` comme défaut de `--skills-dir` au lieu de
  `config.FromEnv().SkillsDir` : `KERN_SKILLS_DIR` est donc ignoré par cette commande.
  `publish-skills` utilise la config. À aligner.

## 2026-07-27 — activity-signal

**A fonctionné**
- Séparer `ActivityReporter` de `HTTPReporter` : les deux ont des exigences opposées
  (l'un synchrone et ordonné entre niveaux, l'autre asynchrone au milieu du travail).
  Une seule classe avec un booléen aurait caché ce désaccord.
- `TestFlushWaitsForSignalsInFlight` compte les requêtes reçues : sans lui, un `Flush()`
  oublié passe vert en local parce que le sink de test répond en une milliseconde.
- Le sink lent (`newSlowSink`) prouve que `Report` ne bloque pas — une assertion qu'aucun
  test de contenu ne peut faire.

**À surveiller**
- `newRunner` est appelé avant `newRunID()` : tout hook ayant besoin de l'identité du run
  doit passer par un relais rempli après coup, pas par la construction du runner.
- Le contexte du run est déjà annulé quand le dernier agent s'arrête. Tout rapport de fin
  doit se détacher (`context.WithoutCancel`), sinon il n'est jamais envoyé.

## 2026-07-28 — failing-node

**A fonctionné**
- `LevelError.Error()` reproduit exactement l'ancien message : le type de l'erreur a changé
  sans qu'aucun test existant ne bouge. Changer la structure sans changer la surface.
- Collecter toutes les erreurs du niveau avant d'en renvoyer une : `wg.Wait()` les avait
  déjà toutes, n'en garder qu'une était une perte gratuite.

**À surveiller**
- Troisième fois qu'une donnée existe dans kern-orch et se perd avant le contrat (`skill`,
  puis l'id du nœud en échec). Avant d'écrire un champ, chercher s'il n'est pas déjà calculé
  quelque part et jeté.

## 2026-07-28 — async-step-reporter

**A fonctionné**
- Mesurer avant/après avec un vrai puits lent plutôt que de déduire le gain. C'est la mesure
  qui a révélé que le premier jet déplaçait l'attente du moteur vers la sortie du processus
  au lieu de la supprimer.
- Écrire le test d'ORDRE avant le code : c'est lui qui a imposé une file à un consommateur
  unique plutôt qu'une goroutine par événement, qui aurait perdu des frontières en silence.

**À surveiller**
- Ordre requis ou non : l'activité tolère le désordre (garde par horodatage), les steps non
  (repliés en séquence). Deux contrats, deux modèles de livraison — ne pas copier l'un sur
  l'autre sans se poser la question.
- Rendre asynchrone une livraison invalide tout test qui affirmait juste après l'appel. Neuf
  ici. C'est le signe que le changement est réel, pas une gêne.

## 2026-07-28 — nested-runs

**A fonctionné**
- Faire porter au nœud sa propre référence de fichier plutôt que de la chercher dans le
  graphe : la recherche marchait à la profondeur 1 et aurait silencieusement échoué au-delà.
  Un bug qui ne se voit pas dans les tests d'un seul niveau.

**À surveiller**
- Tout ce qui se branche via la `Registry` doit être posé AVANT `LoadFile` : les nœuds
  reçoivent leurs options à la construction, pas à l'exécution.

## 2026-07-28 — mode démon

**A fonctionné**
- Poser l'interface `Runner` côté transport (`internal/daemon`) AVANT l'implémentation
  côté orchestration (`internal/cmd`) : le paquet HTTP s'est écrit et testé avec un faux,
  sans jamais toucher un graphe réel. Aucune dépendance croisée à démêler ensuite.
- Refuser de synchrone sur un graphe invalide plutôt que de le découvrir dans une
  goroutine invisible : le test `TestDaemonRunnerFailsFastOnABadGraph` a forcé cette
  décision avant l'implémentation.
- Le marqueur `queued` à l'étape -1 : choisi pour être inférieur à toute étape réelle
  (qui démarre à 0 et ne fait que croître), donc jamais de collision de clé à réfléchir.

**À surveiller**
- Cette daemon mode est le PRÉREQUIS de C5 (exposition des tools), pas C5 elle-même. Ne
  pas confondre « kern-orch peut tourner en continu » avec « kern-ui peut lire un outil » —
  ce sont deux contrats différents, le second reste à écrire.
- Un run lancé par le démon persiste après l'arrêt du process (checkpoint SQLite), mais rien
  ne le relance automatiquement au redémarrage. `resume` reste manuel — pas encore de
  reprise automatique des runs `running` trouvés au démarrage de `serve`.

## 2026-07-29 — tool-invocation (clôture EPIC-03)

**A fonctionné**
- Réutiliser tel quel le protocole subprocess d'`agentrunner` (stdin JSON → stdout JSON,
  pattern `TestHelperProcess`) plutôt que d'en inventer un nouveau pour les tools : même
  vérification (avant de spawner) et même style de test, une seule idée à retenir.

**À surveiller** *(générique — remonté aussi dans `pieges.md`)*
- Une struct Go sans tag `json:"..."` sérialise en PascalCase. `go test` ne le voit jamais
  (comparaison sur la struct, pas sur le JSON brut) — seul un vrai `curl` contre le serveur
  l'a révélé, alors que `internal/checkpoint.Record`/`Summary` ont le même défaut, non
  détecté faute d'un client externe qui les ait encore lus au clavier.
- `checkpoint.Record`/`Summary` restent PascalCase sur le fil — noté ici mais volontairement
  pas corrigé maintenant : personne ne les consomme encore, corriger en dehors du périmètre
  demandé aurait été un changement non sollicité sur un contrat déjà « in use » côté C1-C4.

## 2026-07-29 — c6-steer

**A fonctionné**
- Chercher d'abord si une mécanique existante couvrait déjà le besoin, avant d'en écrire
  une nouvelle : l'approbation réutilise le concurrency model du moteur (un nœud lent bloque
  déjà sa goroutine), `stop` réutilise le chemin d'échec (`exec.CommandContext` tue déjà un
  subprocess sur annulation de contexte). Deux des trois opérations de C6 n'ont ajouté
  aucune mécanique au moteur — seulement `nudge` (le hook `OnBeforeLevel`) était réellement
  neuf.
- Vérifier en réel avec `curl` a immédiatement révélé un bug de concurrence
  (`SQLITE_BUSY`) que `go test` seul (bases isolées par test) ne pouvait pas voir : lire le
  checkpoint pendant qu'un run l'écrit est un accès concurrent que rien n'exerçait avant
  C6. La dette était déjà notée le tout premier jour du projet (2026-07-20, bootstrap) et a
  fini par mordre exactement là où elle avait été prédite.

**À surveiller**
- Un run stoppé ou dont un nœud échoue ne persiste jamais `checkpoint.StatusFailed` — la
  constante existe mais n'est écrite nulle part ; le dernier checkpoint garde le statut
  `running`. La preuve de `stop` dans les tests passe par l'absence de mailbox après coup,
  pas par un statut lisible en base. Un jour où quelqu'un affiche l'état d'un run stoppé
  depuis kern-ui, ce trou devra se combler.
- `nudge`/`decide`/`stop` sur un run existant mais pas "vivant" (déjà terminé, ou pas encore
  repris après un redémarrage) renvoient tous la même erreur générique 400. Pas encore un
  statut HTTP dédié — acceptable pour une V1, à revisiter si un client a besoin de distinguer
  les cas.
- **Trouvé en vérifiant dans un vrai navigateur** : un run parké sur un nœud approval ne
  rapportait RIEN tant qu'aucun niveau n'était terminé — et un niveau où l'approval est le
  nœud d'entrée ne se termine jamais avant la décision. kern-ui n'avait donc aucun moyen de
  montrer la décision en attente. Corrigé en réutilisant le signal d'activité (C10) pendant
  l'attente — mais ça ne résout que la moitié : la TOPOLOGIE (donc l'identification du nœud
  comme `approval`) ne voyage toujours que sur le premier step event, qui n'arrive toujours
  pas tant que rien n'a été décidé si l'approval est le nœud d'entrée. `examples/steer.yaml`
  a été réordonné (un nœud avant l'approval) plutôt que de complexifier le protocole — ça
  correspond de toute façon à la forme réelle la plus probable (un agent décide quelque
  chose, puis un humain le confirme). **Restriction connue et non résolue** : un graphe où
  l'approval est littéralement le premier nœud n'a aujourd'hui aucun chemin d'interface pour
  être décidé — seul l'API directe le peut.
