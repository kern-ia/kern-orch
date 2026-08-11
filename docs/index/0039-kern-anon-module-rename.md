---
id: 0039
feature: kern-anon-module-rename
branch: chore/kern-anon-module-rename
status: done
files:
  - go.mod
  - internal/cmd/courtage_anon.go
  - internal/cmd/courtage_anon_test.go
  - internal/cmd/courtage_ner_noop.go
  - internal/cmd/courtage_ner_onnx.go
  - internal/cmd/runtime.go
tests: []
decisions:
  - "2026-08-11 : suivi de la migration d'organisation GitHub de Kern-Anon (github.com/YoLaub/PresidioGo → github.com/kern-ia/kern-anon, vue au push lors du travail C13/C14 côté Kern-UI le même jour) — go.mod et tous les imports Go alignés sur le nouveau chemin de module."
---

**Quoi** : `go.mod` (`require`/`replace`) et les 5 fichiers de `internal/cmd/` qui
importaient `github.com/YoLaub/PresidioGo/...` pointent maintenant sur
`github.com/kern-ia/kern-anon/...`, le nom réel du module Kern-Anon
(`go.mod` : `module github.com/kern-ia/kern-anon`).

**Pourquoi** : `go build ./...` échouait sur ce poste depuis un moment (confirmé via `git
stash` que `dev` seul, sans aucun changement de cette session, ne compilait déjà pas) —
`internal/cmd` importait l'ancien chemin `github.com/YoLaub/PresidioGo`, mais le `replace`
local pointant vers `../Kern-Anon` ne peut résoudre les imports internes de ce module
(`analyzer.go` important `github.com/kern-ia/kern-anon/contextaware`, etc.) que si le
chemin requis correspond exactement au nom que le module cible se donne lui-même — un
`replace` local ne renomme pas un module, il redirige seulement où le trouver.

**Vérifié en réel** : `go mod tidy` propre (aucun changement de `go.sum`, attendu pour un
remplacement local), `go build ./...` **réussit maintenant en entier** pour la première
fois de cette session, `go test ./...` vert sur les 12 packages — y compris `internal/cmd`,
jamais vérifié par un vrai build/test lors du travail C14 (`0038-activity-narration.md`),
qui confirme rétroactivement que le fil `activityRelay`/`wireApproval`/`serve.go` de ce
changement compile et passe ses tests.
