---
id: 0040
feature: sub-agent-creation
branch: feature/sub-agent-creation
status: done
files:
  - internal/skills/skills.go
  - internal/skills/write.go
  - internal/config/config.go
  - internal/daemon/router.go
  - internal/cmd/serve.go
  - internal/cmd/runtime.go
  - internal/cmd/commands.go
tests:
  - internal/skills/skills_test.go
  - internal/skills/write_test.go
  - internal/daemon/skills_test.go
decisions:
  - "2026-08-11/12 : réouverture de la décision « hors POC » du 2026-07-28 (docs/a-trancher.md §4) — le project owner a tranché les 4 questions restées ouvertes plutôt que de les laisser reportées."
  - "2026-08-12 : v1 = chaîne linéaire d'agents seulement, pas de nœud de validation humaine — trouvaille en lisant le moteur : un `router:` est une fonction Go enregistrée une par une à la compilation (internal/cmd/runtime.go), pas déclaratif. Un branchement demanderait un routeur générique côté moteur, non construit ici."
  - "2026-08-12 : deux étages (skills/ vs skills-custom/), kern-orch écrit (nouveau endpoint authentifié, pas kern-pilot qui n'existe pas), tout compte connecté crée, tout le monde voit, seul le créateur supprime."
---

**Quoi** : `POST /api/v1/skills` et `DELETE /api/v1/skills/{name}` sur le daemon
(`internal/daemon`). Écrit un `SKILL.md` (+ un `graph.yaml` chaîné si plus d'une étape)
dans un second dossier (`KERN_SKILLS_CUSTOM_DIR`, défaut `skills-custom/`), jamais dans
`KERN_SKILLS_DIR` — une mise à jour du produit ne peut jamais écraser une création.
`skills.LoadMerged` fusionne les deux dossiers (le livré gagne en cas de collision de
nom), et remplace `skills.Load` partout où le daemon lisait le catalogue (`Dispatch`,
`ListTools`, `InvokeTool`, la publication au démarrage) — une compétence créée est
immédiatement dispatchable, listée comme outil si pertinente, et republiée vers kern-ui
sans redémarrage.

**Pourquoi** : C11, explicitement reporté hors POC le 2026-07-28
(`Kern-UI/docs/a-trancher.md` §4). Le project owner a rouvert la décision cette session et
tranché les quatre questions que le document laissait explicitement en suspens.

**Ce qui a changé par rapport à l'intention initiale** : le v1 n'inclut pas de nœud de
validation humaine dans les chaînes créées — pas un choix de produit, une contrainte
moteur trouvée en lisant `internal/topology/loader.go` : un `router:` référence une
fonction Go enregistrée statiquement (`reg.Router("onConfirmDecision", ...)`), une par
approbation, compilée dans le binaire. Aucune façon aujourd'hui pour un graphe créé sans
toucher au code Go de brancher sur une décision. Une chaîne d'agents non-conditionnelle
(`type: agent` + `to:`), en revanche, est déjà 100% déclarative — zéro changement moteur.
Confirmé avec l'utilisateur avant de coder (AskUserQuestion) : la validation humaine reste
un v2, qui commencera par concevoir ce routeur générique, pas tenté ici.

**Vérifié en réel** : `go build ./...` et `go test ./...` verts sur les 12 packages. Un
vrai daemon (`kern-orch serve`, arbre isolé sous `/tmp`, aucune LLM réelle configurée —
`agentrunner.Stub`) : `POST /api/v1/skills` avec deux étapes écrit un vrai `SKILL.md` +
`graph.yaml` sur disque, `POST /api/v1/dispatch` sur ce nom **sans redémarrer le daemon**
lance un run réel qui traverse les deux nœuds dans l'ordre (`Step: 2`, `Status: "done"`,
état final portant l'écho du second nœud). Suppression testée : un compte différent du
créateur reçoit 403 et le fichier reste sur disque ; le créateur reçoit 200 et le dossier
disparaît.

**Reste à faire, côté Kern-UI** : proxy `internal/steer`/`internal/httpapi`, éditeur
no-code dans le Grimoire (`web/src/grimoire/`), contrôle de suppression visible seulement
au créateur — pas commencé dans ce commit.
