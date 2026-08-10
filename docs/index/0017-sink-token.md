---
id: 0017
feature: sink-token
branch: feature/sink-token
status: done
files:
  - internal/report/http.go
  - internal/report/activity.go
  - internal/report/registry.go
  - internal/config/config.go
  - internal/cmd/commands.go
  - internal/cmd/runtime.go
tests:
  - internal/report/queue_test.go
decisions:
  - "2026-07-28 : UN seul `KERN_SINK_TOKEN` pour les trois URL — trois contrats vers le même consommateur ; demander trois secrets produirait surtout trois copies du même"
  - "2026-07-28 : jeton vide = AUCUN en-tête, pas un bearer vide — un sink doit voir un appelant anonyme, pas un appelant malformé"
  - "2026-07-28 : un 401 du sink ne casse pas la mission — la règle « rapporter n'est jamais porteur » vaut aussi pour l'authentification"
---

**Quoi** : kern-orch présente un jeton porteur sur les trois contrats qu'il émet.

**Vérifié en réel** : sans jeton contre un kern-ui protégé, chaque rapport reçoit 401 et
**le run se termine quand même**, avec une ligne sur stderr.
