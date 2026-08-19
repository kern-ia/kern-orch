---
id: 0045
feature: state-projection-from-events
branch: feature/state-projection
status: done
files:
  - internal/journal/projection/projection.go
tests:
  - internal/journal/projection/projection_test.go
decisions:
  - "2026-08-19 : `Project` vit dans `internal/journal/projection`, pas dans `internal/journal` — l'issue 05 va faire émettre des événements par le moteur, donc `internal/graph` importera `internal/journal` ; une projection placée dans `internal/journal` et important `internal/graph` refermerait la boucle en cycle d'import. Un package frère dépend des deux feuilles et respecte le sens unique exigé par CONVENTIONS.md."
  - "2026-08-19 : un Freeze est rejoué comme un REMPLACEMENT — un `graph.State` neuf repeuplé depuis `CarriedOver`, tout en zone persistante, `Frozen` incrémenté. `FreezeApplied.Dropped` n'est délibérément pas utilisé : reconstruire « l'état précédent moins Dropped » est le modèle en delta que les Notes de l'epic dénoncent ; il coïncide par accident avec `DefaultCarryOver` et se trompe dès qu'un carry-over re-zone, réécrit ou synthétise une valeur."
  - "2026-08-19 : la règle de combinaison est lue sur `LevelClosed.Rule`, jamais déduite du nombre de nœuds observés. Le seul endroit où les deux nombres sont comparés est un refus : `replace` sur autre chose qu'une branche unique ne désigne aucune branche à adopter, donc erreur plutôt que résolution arbitraire."
  - "2026-08-19 : les branches sont matérialisées dès `LevelOpened` (un `Clone` de l'état partagé par nœud du frontier) et repliées dans l'ORDRE DU FRONTIER, pas dans l'ordre du journal — les goroutines de niveau terminent dans un ordre quelconque, donc l'ordre du journal n'est pas celui du run et déciderait mal le gagnant d'une clé disputée."
  - "2026-08-19 : `NudgeApplied` s'applique à l'état partagé et est refusé à l'intérieur d'un niveau ouvert (les branches sont déjà clonées, il n'y a plus d'état sur lequel il puisse atterrir honnêtement) ; `FreezeApplied` s'applique à l'état partagé hors niveau, à la branche unique d'un frontier à un nœud, et est refusé dans un fan-out — il n'y porte aucune attribution de nœud et le moteur le perdrait de toute façon (`Merge` ne transporte ni suppressions ni compteur `Frozen`)."
  - "2026-08-19 : une queue interrompue (dernier niveau ouvert jamais fermé) n'est pas une erreur et ne combine rien — la projection s'arrête au dernier état réellement combiné. Fermer cette queue est le travail de l'issue 08 ; l'anticiper ici inventerait un état que le run n'a pas eu."
---

**Quoi** : nouveau package `internal/journal/projection` avec une unique fonction pure
`Project(events []journal.Event) (*graph.State, error)`. Elle rejoue une tranche ordonnée
d'événements et reconstruit l'état partagé : clés produites par nœud (avec zones), règle de
combinaison par niveau, freeze, nudge, compteurs `Step` et `Frozen`. Aucune E/S : lire les
événements en SQLite est le travail de l'issue 07, fermer une queue interrompue celui de
l'issue 08.

**Pourquoi** : quatrième issue de l'Epic 1 (#8). C'est la fonction qui rend l'état *projection*
du journal plutôt que registre tenu à côté ; si elle est fausse, « le journal est la source de
vérité » est un slogan. Deux faits que le rejeu ne peut pas re-dériver sont donc lus sur les
événements : la règle de combinaison (`runLevel` **remplace** l'état sur un frontier à un nœud
et **fusionne** additivement au-delà, et les deux peuvent laisser le même ensemble de clés) et
ce qu'un Freeze a gardé (`State.Freeze` remplace le contenu en bloc et réinitialise la carte
des zones).

**Vérifié en réel** : `go build ./...`, `go vet ./...` et `go test ./...` verts sur les
14 packages. 23 tests dans `internal/journal/projection`. Le test du carry-over **non par
défaut** est vérifié non vacant : implémentée en delta (« l'état précédent moins `Dropped` »),
la projection continue de passer le test `DefaultCarryOver` et le test de propagation d'une
suppression sous `replace`, et échoue sur ce seul test — elle rend
`{"scratch":"notes"}` avec `scratch` resté en zone `ephemeral`, là où le `graph.State` réellement
gelé vaut `{"scratch":"notes","summary":"1 dossier"}` toutes clés en zone persistante. Le test
de l'ordre du frontier est vérifié de la même façon : replier les branches à l'envers le fait
échouer (`shared:"from-b"` au lieu de `from-a`). Aucun fichier de `internal/graph`,
`internal/checkpoint` ou `contracts/` n'est touché.
