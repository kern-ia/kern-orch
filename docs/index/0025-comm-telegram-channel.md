---
id: 0025
feature: comm-telegram-channel
branch: feature/comm-telegram-channel
status: done
files:
  - skills/community-management-agency/agent_cli.py
  - skills/community-management-agency/SKILL.md
tests:
  - skills/community-management-agency/agent_cli_test.py
decisions:
  - "2026-08-06 : Telegram est le premier canal de community-management-agency avec un vrai connecteur (les autres restent 'propose, l'humain publie'). send_telegram() (stdlib urllib, aucune dépendance ajoutée) est appelée par run_publieur UNIQUEMENT si confirm_publication est approuvé ET que le canal Telegram est détecté ET que les identifiants sont configurés — sinon retombe sur le garde-fou G2 habituel (aucun envoi, signalement explicite)."
  - "2026-08-06 : la détection du canal (TELEGRAM_PLATFORM_RE) lit la ligne '**Plateforme(s)** :' du brief éditorial du stratège plutôt que d'ajouter un marqueur structuré supplémentaire — plus fragile qu'un marqueur strict (type MODE_RE), mais suffisant puisque le geste qui déclenche vraiment l'envoi reste la lecture humaine du contenu avant validation, pas cette détection."
  - "2026-08-06 : BUG RÉEL trouvé en vérifiant en direct — le premier essai est tombé sur le garde-fou G2 alors que Telegram était bien la plateforme choisie. Cause : le stratège écrit '**Plateforme(s)**' en gras markdown, les astérisques entre le libellé et les deux-points cassaient le regex initial ('plateforme\\(s\\)\\s*:'). Corrigé ('plateforme\\(s\\)[*_\\s]*:'), avec un test de non-régression sur le cas exact rencontré."
---

**Quoi** : `community-management-agency` peut maintenant publier réellement sur Telegram
quand l'utilisateur valide, au lieu de seulement le proposer.

**Vérifié en réel** : deux dispatches réels. Le premier a révélé le bug markdown (message
correctement refusé par le garde-fou plutôt que silencieusement perdu — le filet de
sécurité a fonctionné). Corrigé, puis un second dispatch a réellement envoyé un message
sur le vrai canal Telegram configuré, confirmé par le `message_id` renvoyé par l'API.
`pytest` (14 tests, y compris le cas de régression markdown) vert, `go test ./...` vert
(aucune logique Go modifiée, contenu de prompt + un module Python seulement).

**Pièges** : voir décision "BUG RÉEL" ci-dessus — générique et à surveiller ailleurs si
une future détection de contenu se base sur du texte généré par un modèle qui écrit
naturellement en markdown.
