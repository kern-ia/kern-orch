---
id: 0055
feature: capture-real-cli-transcripts
branch: feature/capture-real-cli-transcripts
status: done
files:
  - internal/agentrunner/testdata/claude-code-stream.jsonl
  - internal/agentrunner/testdata/opencode-message.json
  - internal/agentrunner/testdata/README.md
tests: []
decisions:
  - "2026-08-20 : le sous-processus `claude -p --input-format=stream-json` hérite des
    identifiants OAuth/abonnement de la session Claude Code parente — aucune
    `ANTHROPIC_API_KEY` n'était présente dans l'environnement, mais l'appel a quand
    même réussi (`\"apiKeySource\":\"none\"` dans l'évènement `system`/`init` capturé),
    confirmant que le sous-processus n'invente pas ses propres identifiants."
  - "2026-08-20 : `opencode` n'était pas installé ; installé proprement en local via
    `npm install -g opencode-ai@latest` (toolchain node géré par mise, donc
    entièrement dans le home utilisateur, sans sudo). Aucune clé API de provider
    n'était configurée (`opencode auth list` → 0 credentials), mais `opencode` expose
    un provider hébergé gratuit sans identifiant (`opencode`/`big-pickle`,
    `options.apiKey:\"public\"`) qui a servi de capture réelle plutôt qu'une
    fabrication."
  - "2026-08-20 : `opencode-message.json` n'est pas le corps brut du seul appel
    `POST /session/:id/message` (qui ne renvoie que le dernier message) mais celui
    d'un `GET /session/:id/message` de suivi sur la même session, qui restitue les
    trois messages réels (texte utilisateur, appel d'outil `bash` réel, réponse
    texte finale) — nécessaire pour que la fixture porte les deux formes qu'un
    traducteur (issues 04/05) doit distinguer, sans qu'aucun octet du fichier ne
    soit inventé. Écart documenté explicitement dans `testdata/README.md`."
  - "2026-08-20 : seules les valeurs suivantes ont été rédigées : les chemins
    absolus réels de la machine de capture (`/Users/yoann/...`) et un chemin de
    socket temporaire — le contenu et la structure des messages restent inchangés."
---

**Quoi** : deux fixtures réelles et vérifiées sous
`internal/agentrunner/testdata/` — `claude-code-stream.jsonl` (deux captures
concaténées d'un vrai `claude -p --input-format=stream-json` : une réponse texte et
un aller-retour d'appel d'outil `Bash`) et `opencode-message.json` (transcript réel
d'une session `opencode serve`, même deux formes) — plus un `README.md` documentant
la commande exacte, la version de CLI et la procédure de reproduction pour chacune.
Aucune ligne n'est écrite à la main : chaque octet vient d'un vrai processus/serveur
qui a réellement tourné.

**Pourquoi** : première issue de l'Epic 2 (#33), bloquante pour les issues 04 et 05
qui écriront la logique de traduction contre ces fixtures. Le protocole actuel
d'`agentrunner` (`protocol.go`) est un placeholder jamais confronté à un vrai CLI ;
cette issue existe pour ne pas répéter la même erreur un niveau plus bas.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts sur
tous les paquets. Les deux fixtures sont validées comme JSON(-lines) syntaxiquement
correct par un parseur JSON réel (pas une relecture visuelle). Aucune chaîne en
forme de clé (`sk-...`, `Bearer ...`) ne subsiste après rédaction, vérifié par grep
dédié sur les deux fichiers avant commit.
