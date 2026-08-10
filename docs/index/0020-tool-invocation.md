---
id: 0020
feature: tool-invocation
branch: feature/epic03-tool-invocation
status: done
files:
  - internal/skills/skills.go
  - internal/tools/tools.go
  - internal/tools/validate.go
  - internal/tools/runner.go
  - internal/daemon/router.go
  - internal/cmd/serve.go
  - skills/greeting/SKILL.md
  - skills/greeting/tool.py
tests:
  - internal/skills/skills_test.go
  - internal/tools/validate_test.go
  - internal/tools/runner_test.go
  - internal/daemon/router_test.go
decisions:
  - "2026-07-29 : exécution en subprocess (comme les agents), pas en func Go enregistrée — un tool s'ajoute sans toucher kern-orch."
  - "2026-07-29 : kern-ui lira les tools via kern-orch, pas un service séparé — une seule URL, cohérent avec C1/C4/C10."
  - "2026-07-29 : argument d'entrée dès la V1 (schéma Params déclaré en frontmatter, validé avant de lancer le subprocess)."
  - "2026-07-29 : pas de cache — chaque invocation relance le subprocess, `as_of` = l'instant de l'appel."
---

**Quoi** : clôt EPIC-03 (kern-tools). Un skill `type: tool` déclare `command` (argv) et
`params` (nom/type/requis) en frontmatter ; `internal/tools.Runner` valide l'entrée puis
lance le subprocess (protocole stdin/stdout JSON, une ligne). Exposé par le démon :
`GET /api/v1/tools` (catalogue), `POST /api/v1/tools/{name}/invoke` (valeur affichée :
label, value, as_of). C'est ce que C5 attendait côté kern-ui.

**Vérifié en réel** : `kern-orch serve` + `curl` contre un vrai tool Python
(`skills/greeting`) — liste, invocation réussie, param manquant → 400, tool inconnu → 404.

**Pièges** : `tools.Result`/`Spec` sans tag JSON sortaient en PascalCase (`Label`, `AsOf`)
— invisible en test unitaire (comparaison sur la struct Go), seul un vrai `curl` l'a
montré. Toujours vérifier la casse JSON réelle d'un nouveau contrat au clavier, pas
seulement via `json.Unmarshal` dans le test.
