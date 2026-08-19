---
id: 0044
feature: journal-store-append-read
branch: feature/journal-store
status: done
files:
  - internal/checkpoint/journal.go
  - internal/checkpoint/sqlite.go
tests:
  - internal/checkpoint/journal_test.go
  - internal/checkpoint/schema_test.go
decisions:
  - "2026-08-19 : la séquence d'un run commence à FirstSeq = 1, pas à 0 — 0 est la valeur zéro de Go, donc un Event dont le Seq n'a jamais été assigné passerait pour l'ouverture légitime d'un journal et décalerait toute la suite d'un cran sans que rien ne le signale."
  - "2026-08-19 : Append vérifie le next-seq stocké au lieu de l'assigner — le journal fait respecter la séquence, il ne la fabrique pas. Conséquence assumée : deux goroutines qui appendent sur le MÊME run se voient refuser l'une des deux (ErrSeqMismatch) plutôt que renumérotées ; renuméroter casserait l'idée que l'émetteur se fait de ce qu'il a écrit. Un run a un seul appender ; des runs différents sont concurrents sans contrainte."
  - "2026-08-19 : contrôle de contiguïté en deux temps — le premier seq du batch doit égaler le next-seq stocké, et le batch doit être contigu en interne. Le seul contrôle du premier seq laisserait passer un trou à l'intérieur d'un batch ; un journal qui peut se trouer est un journal dont le rejeu n'est pas fiable."
  - "2026-08-19 : encodage JSON de tout le batch AVANT d'ouvrir la transaction — un payload non sérialisable échoue sans rien écrire et sans avoir pris de verrou d'écriture sur une base partagée. Encoder ligne par ligne dans la transaction serait correct aussi, mais seulement grâce au rollback, et après avoir bloqué les autres accès."
  - "2026-08-19 : l'Event entier est stocké en une colonne JSON (`event`), run_id/seq/at étant seulement extraits en colonnes d'index — internal/journal reste l'unique propriétaire de l'encodage : un nouveau type de payload n'impose aucune migration ici et aucun second décodeur ne peut dériver du premier."
  - "2026-08-19 : PRIMARY KEY (run_id, seq) plutôt que de se reposer sur le SetMaxOpenConns(1) d'OpenSQLite — le pin de pool rend bien le read-then-write atomique, mais il est invisible depuis journal.go ; la clé primaire est la garantie réelle, un écrivain qui passerait le contrôle ne peut pas écraser un événement existant."
  - "2026-08-19 : SchemaVersion passe à 2 via le mécanisme de la fiche 0042 (table ajoutée au même const `schema`, même transaction de création/estampille) plutôt qu'un second mécanisme — une base d'avant le journal est refusée dans les deux sens, ancienne comme récente."
  - "2026-08-19 : le vrai travail vit dans appendTx(ctx, *sql.Tx, ...), Append n'étant que le wrapper qui ouvre sa propre transaction — c'est ce qui permettra à l'issue 07 d'écrire le journal et le cache de projection dans une seule transaction sans que la frontière transactionnelle d'Append gêne. L'enveloppement lui-même n'est pas fait ici."
---

**Quoi** : la moitié durable du journal, dans `internal/checkpoint`. Une table `events`
append-only clé `(run_id, seq)`, et trois opérations sur `*SQLiteStore` derrière un port
`Journal` distinct de `Store` : `Append(ctx, runID, events...)` (batch en une transaction,
refusé si la séquence ne continue pas exactement celle stockée), `Read(ctx, runID)` et
`ReadFrom(ctx, runID, fromSeq)`. Sentinelles `ErrSeqMismatch`, `ErrRunIDMismatch`,
`ErrInvalidSeq`. `SchemaVersion` passe de 1 à 2.

**Pourquoi** : troisième issue de l'Epic 1 (#7). Tout ce qui suit dans l'epic écrit à travers
ce contrat d'append — l'émission moteur (05), les mutations hors nœud (06), l'écriture
atomique avec la projection (07). Une séquence qui peut se trouer silencieusement est un
journal dont le rejeu ne reproduit pas le run, donc le refus bruyant est la propriété
centrale, pas un garde-fou secondaire. C'est aussi le deuxième chemin d'écriture sur
la base que le daemon partage déjà entre les endpoints de pilotage et la goroutine d'un run
vivant, d'où le `-race` explicite.

**Vérifié en réel** : `go build ./...` et `go vet ./...` verts. `go test ./...` vert sur les
13 packages ; `internal/checkpoint` compte 40 tests dont 23 nouveaux. `go test -race
./internal/checkpoint/...` vert, y compris à `-count=3` : un test appende depuis une
goroutine pendant qu'une autre lit en boucle et vérifie ne voir que des préfixes denses, un
autre fait appender trois runs concurremment et vérifie que chacun garde sa séquence 1..40.
`go test -race ./...` est vert sur tout le dépôt — première fois. Contrôle de mutation : en
neutralisant le contrôle de contiguïté intra-batch, le test correspondant échoue bien, donc
il n'est pas vacant.
