---
id: 0041
feature: registry-custom-fields
branch: feature/registry-custom-fields
status: done
files:
  - internal/report/registry.go
  - README.md
tests:
  - internal/report/registry_test.go
decisions:
  - "2026-08-14 : kern.registry/v1 gagne custom/created_by (additif, omitempty) — sans eux, le Grimoire ne peut pas savoir quelle compétence proposer à la suppression ni à qui. Exception délibérée à « ce qui ne voyage pas », documentée dans le commentaire de CatalogueEntry."
---

**Quoi** : `CatalogueEntry` (`kern.registry/v1`) gagne deux champs optionnels : `custom`
(vrai pour une compétence créée via C11) et `created_by` (le compte créateur). Additif,
`omitempty` — la fixture `contracts/kern.registry.v1.json` reste inchangée, un skill livré
ne porte ni l'un ni l'autre sur le fil.

**Pourquoi** : trouvé en construisant l'éditeur no-code du Grimoire côté Kern-UI (C11) —
`CatalogueEntry` était délibérément plus étroit que `skills.Skill` (`docs/index/
0040-sub-agent-creation.md`), et cette étroitesse cachait un vrai besoin : sans ces deux
champs, kern-ui n'a aucun moyen de savoir quelle entrée du catalogue est supprimable ni
par qui. Documenté comme exception explicite dans le commentaire de `CatalogueEntry`,
pas une régression de la discipline « ce qui ne voyage pas ».

**Vérifié en réel** : `go test ./internal/report/...` vert, y compris le test fixture
existant (`TestPublisherEmitsTheRegistryFixture`, inchangé) et deux nouveaux — les champs
voyagent quand `Custom`/`CreatedBy` sont renseignés, restent absents (jamais une valeur
vide) sinon. `go build ./...`/`go test ./...` verts sur les 12 packages.
