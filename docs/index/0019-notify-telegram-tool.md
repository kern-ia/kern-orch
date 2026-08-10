---
id: 0019
feature: notify-telegram-tool
branch: feature/notify-telegram-tool
status: done
files:
  - internal/notify/client.go
  - internal/notify/tool.go
  - internal/config/config.go
  - internal/cmd/runtime.go
  - internal/cmd/serve.go
  - examples/notify.yaml
tests:
  - internal/notify/client_test.go
  - internal/notify/tool_test.go
  - internal/config/config_test.go
decisions:
  - "2026-07-29 : ce tool est l'aller (agent → humain), distinct du relais kern-notify (C12, l'état → humain). Pas de dépendance entre les deux dépôts : le client Telegram est réécrit ici, pas importé."
  - "2026-07-29 : tool non configuré (jetons absents) ÉCHOUE le nœud plutôt que d'avaler le message — même règle que kern-exec (refuser plutôt que dégrader silencieusement)."
  - "2026-07-29 : le message part de l'état (clé `message`), pas d'un argument du nœud YAML — c'est ce que l'agent écrit dans l'état qui part, pas un texte figé dans la topologie."
  - "2026-07-29 : sens entrant (Telegram → agent, tâches/documents) explicitement hors périmètre — c'est C6 kern-pilot, pas construit."
---

**Quoi** : builtin tool `notify` — un nœud `type: tool, func: notify` envoie la clé d'état
`message` vers Telegram. Configuré par `KERN_TELEGRAM_BOT_TOKEN`/`KERN_TELEGRAM_CHAT_ID` ;
un agent qui écrit dans l'état puis route vers ce nœud décide lui-même de prévenir un humain.

**Vérifié en réel** : `go run . run examples/notify.yaml` avec les vrais jetons — message
reçu sur le vrai chat Telegram, pas seulement un serveur de test.
