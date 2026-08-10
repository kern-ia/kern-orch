---
id: 0036
feature: courtage-upload-backend
branch: feature/courtage-upload-ui
status: done
files:
  - internal/daemon/router.go
  - internal/daemon/upload.go
  - internal/config/config.go
  - internal/cmd/serve.go
tests:
  - internal/daemon/upload_test.go
---

**Quoi** : `POST /api/v1/uploads` (multipart, champ `file`) sur le daemon `kern-orch`,
sauvegarde le document sous `KERN_ORCH_UPLOAD_DIR` (défaut `./data/uploads`) et retourne
son chemin local — même convention "le texte EST le chemin du document" qu'une commande
de chat ou le listener Telegram de `courtage-extraction`. Aucune nouvelle mécanique
d'ingestion : un troisième vrai appelant du même point d'entrée (`POST
/api/v1/dispatch`).

**Vérifié en réel** : `go test ./...` vert. Un vrai upload `curl -F file=@...` a produit
un fichier réel sur disque, puis un vrai dispatch avec le chemin retourné a fait tourner
`courtage-extraction` jusqu'à l'extraction réelle du contenu du document uploadé.

**Pièges** : aucun.
