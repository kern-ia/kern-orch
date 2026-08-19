---
id: 0042
feature: sqlite-schema-versioning
branch: feature/sqlite-schema-versioning
status: done
files:
  - internal/checkpoint/sqlite.go
  - internal/checkpoint/store.go
tests:
  - internal/checkpoint/schema_test.go
decisions:
  - "2026-08-19 : la version du schéma vit dans une table schema_meta à une ligne, pas dans PRAGMA user_version — le pragma ne survit pas à un .dump/restore et aucun SELECT ordinaire ne le montre ; une base restaurée se déclarerait non versionnée et serait refusée pour une raison qu'aucune inspection du fichier n'explique. Le refus étant tout le mécanisme, il doit être inspectable par la personne qui le rencontre."
  - "2026-08-19 : refus symétrique — toute version stockée différente de SchemaVersion est une impasse, plus récente comme plus ancienne. Rien ne migre, donc une base plus ancienne est exactement aussi illisible qu'une plus récente. Alternative écartée : adopter silencieusement les bases antérieures au tampon, ce qui est précisément la réinterprétation que cette issue existe pour empêcher."
  - "2026-08-19 : une base écrite avant le mécanisme rapporte la version 0 (unversionedSchema) au lieu d'un cas d'absence — elle tombe dans le même refus que n'importe quel décalage, sans branche spéciale qui finirait par l'adopter."
  - "2026-08-19 : création des tables et pose du tampon dans une seule transaction — séparés, un crash entre les deux laisserait des tables sans tampon, indiscernables d'un fichier pré-versionnement et donc refusées à jamais."
---

**Quoi** : `internal/checkpoint` gagne une constante `SchemaVersion` (1), une table
`schema_meta` à une ligne qui la persiste, et un `ErrSchemaVersion`. `OpenSQLite` crée et
tamponne une base vide, ouvre sans rien réécrire une base déjà à la bonne version, et refuse
toute autre base avec une erreur qui **nomme le fichier**. L'`ALTER TABLE checkpoints ADD
COLUMN dossier` à l'erreur volontairement ignorée est supprimé, ainsi que le commentaire qui
le justifiait par l'absence de mécanisme de migration.

**Pourquoi** : première issue de l'Epic 1 (#5), et elle passe avant tout le reste parce que
chaque issue suivante ajoute ou redéfinit une table. Sans tampon, l'epic aurait inventé un
second mécanisme ad hoc à côté de celui qu'il devait remplacer. Le vrai risque n'est pas
qu'une ouverture échoue : c'est qu'une base écrite sous d'autres règles soit lue comme si les
règles actuelles s'y appliquaient — exactement ce que faisait l'`ALTER TABLE`, qui rattrapait
une colonne manquante et laissait croire que le fichier était à jour. `SetMaxOpenConns(1)` et
le `PRAGMA busy_timeout` restent intacts : la vérification de version passe par le même pool
épinglé.

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts sur les
12 packages ; `go test -race ./internal/checkpoint/...` vert. 17 tests dans
`internal/checkpoint`, dont 4 nouveaux : base fraîche tamponnée à `SchemaVersion` ; base à la
version courante rouverte **sans écriture** (le fichier est passé en lecture seule avant la
seconde ouverture — SQLite refuse alors toute écriture, donc une réouverture réussie *est*
l'assertion) ; base tamponnée à `SchemaVersion+1` refusée avec le chemin du fichier dans le
message ; base pré-versionnement (table `checkpoints` sans colonne `dossier`, pas de
`schema_meta`) refusée au lieu d'être adoptée. Les 13 tests existants passent inchangés.
