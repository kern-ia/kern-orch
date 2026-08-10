---
id: okf-023
feature: serve-publishes-registry
branch: feature/serve-publishes-registry
status: done
files:
  - internal/cmd/serve.go
tests:
  - internal/cmd/serve_publish_test.go
decisions:
  - "2026-08-01 : `serve` publie le catalogue une seule fois, à son propre démarrage —
    pas par mission, pas en tâche périodique. Un daemon tourne des heures ou des jours ;
    republier à chaque dispatch serait un travail répété pour un catalogue qui ne change
    qu'au redémarrage (les skills se lisent au démarrage, pas à chaud)."
---

**Quoi** : `kern-orch serve` appelle désormais `publishRegistry` avant d'ouvrir le port —
même fonction déjà utilisée par `run` et `publish-skills`, best-effort (un sink mort ne
bloque pas le démarrage, juste un `slog.Warn`).

**Pourquoi** : trouvé en préparant la démo du 2026-08-01 — `kern-orch serve` avait tourné
toute une session, des missions réelles passaient, et le Grimoire de kern-ui restait sur
« catalogue non reçu » en permanence. Seules `run` et `publish-skills` appelaient
`publishRegistry` ; `serve`, le mode réellement utilisé en production, ne l'appelait
jamais. Contournable à la main (`kern-orch publish-skills` après coup), mais un opérateur
ne doit pas avoir à connaître ce détail pour voir ses propres compétences.

**Vérifié en réel** : `go test ./...` vert (paquet `cmd` inclus) ; rejoué en vrai contre
kern-ui — le Grimoire affiche `crm-dashboard`, `greeting`, `heartbeat`, `planner`,
`prospection` dès le démarrage de `kern-orch serve`, sans commande manuelle.
