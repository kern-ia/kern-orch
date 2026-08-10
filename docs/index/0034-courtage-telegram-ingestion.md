---
id: 0034
feature: courtage-telegram-ingestion
branch: feature/courtage-telegram-ingestion
status: done
files:
  - skills/courtage-extraction/telegram_listener.py
  - skills/courtage-extraction/SKILL.md
  - skills/specs.md
  - .gitignore
tests:
  - skills/courtage-extraction/telegram_listener_test.py
decisions:
  - "2026-08-07 : processus Python autonome (long-polling getUpdates), PAS un nœud de graphe ni une nouvelle mécanique kern-orch — kern-orch's dispatch API est pure requête/réponse, rien n'y écoute. Le listener appelle POST /api/v1/dispatch, exactement le point d'entrée qu'un humain tapant dans le chat utilise déjà."
  - "2026-08-07 : le listener répond DANS la conversation Telegram qui a envoyé le document — légitime ici (l'expéditeur a initié le contact), contrairement à la relance interne du besoin #3 qui ne peut jamais contacter un client n'ayant jamais écrit au bot en premier."
  - "2026-08-07 : BUG RÉEL trouvé en vérifiant en direct — un document envoyé une fois est arrivé comme DEUX update_id Telegram distincts (~10s d'écart), causant deux téléchargements et deux dispatches. Cause non confirmée (redélivrance Telegram ou retry client) mais non pertinente : un consommateur d'événements externes ne doit jamais supposer une livraison unique. Corrigé par SeenFileIDs (dédoublonnage par file_id, fenêtre glissante de 10 min, taille bornée puisque le processus tourne indéfiniment)."
  - "2026-08-07 : dispatch_extraction ne confirme que l'ACCEPTATION du run (kern-orch répond immédiatement avec un run_id, avant que le pipeline ne s'exécute) — pas son succès final. Le message de confirmation Telegram dit explicitement 'traitement lancé', jamais 'traité avec succès'. Aucune notification de fin/échec asynchrone n'est renvoyée à l'expéditeur — limite connue, consignée, pas cachée."
---

**Quoi** : réception de documents via Telegram pour `courtage-extraction` — un processus
d'écoute séparé (`telegram_listener.py`) qui télécharge les documents reçus et appelle la
même API `POST /api/v1/dispatch` qu'un humain dans le chat, sans toucher à `kern-orch`.

**Vérifié en réel, deux passes** : premier envoi réel — le pipeline a bien démarré, mais le
document est arrivé en double (bug réel, voir décision), causant deux runs pour un seul
envoi. `pytest` vert (11 tests), correction du dédoublonnage, seconde passe : un document
réel envoyé une fois a produit exactement un téléchargement, un dispatch, une confirmation
Telegram reçue dans la même conversation. Auth Telegram (`get_updates`) et dispatch
`kern-orch` (`dispatch_extraction`) vérifiés séparément contre de vrais services avant même
le test de bout en bout.

**Pièges** : voir décision "BUG RÉEL" — leçon générique : un consommateur d'un flux
d'événements externe (webhook, polling, file d'attente) ne doit jamais présumer qu'un
événement n'arrive qu'une fois, quelle qu'en soit la raison. Généralisable au-delà de
Telegram.
