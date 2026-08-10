---
id: 0033
feature: courtage-memo-display
branch: feature/courtage-memo-display
status: done
files:
  - internal/cmd/courtage_anon.go
tests:
  - internal/cmd/courtage_anon_test.go
decisions:
  - "2026-08-07 : displayKey ajouté en paramètre optionnel de newDeanonymizeTool plutôt que de créer un mécanisme séparé — un Go tool node n'a pas accès à son propre node id à l'exécution (ToolFunc ne le porte pas), mais chaque instance concrète (deanonymizePII, deanonymizeMemoOutput) tourne à un seul node id fixe par construction : le coder en dur au point d'appel est exact, pas une supposition."
  - "2026-08-07 : SEUL deanonymizeMemoOutput reçoit une display key (display:demasquage_memo) — deanonymizePII (besoin #1) n'en reçoit pas, seul le mémorandum a été demandé ; le dossier d'extraction reste réservé au contexte de validation de confirm_extraction."
---

**Quoi** : le mémorandum final (démasqué) était invisible dans Kern-UI — seulement
consultable via l'état brut du run (`GET /api/v1/runs/{id}`). `demasquage_memo` écrit
maintenant `display:demasquage_memo`, la convention que le panneau de la Ruche lit déjà
pour n'importe quel nœud (`Kern-UI/web/src/runs/HiveGraph.tsx`, `outputOf`) — aucun
changement côté Kern-UI nécessaire, la convention existait déjà et n'attendait qu'un
producteur.

**Vérifié en réel** : `go test ./...` vert. Un vrai dispatch HTTP de bout en bout
(extraction → nudge notes d'entretien → approbation → mémorandum) confirme
`display:demasquage_memo` présent et peuplé du texte réel du mémorandum démasqué dans
l'état du run, à l'endroit exact où `confirm_memo` attend la validation humaine.

**Pièges** : aucun.
