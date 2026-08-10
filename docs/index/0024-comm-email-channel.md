---
id: 0024
feature: comm-email-channel
branch: feature/comm-email-channel
status: done
files:
  - skills/community-management-agency/agent_cli.py
  - skills/community-management-agency/SKILL.md
tests: []
decisions:
  - "2026-08-06 : crew-comm/agents/strategiste.py ne connaît que les réseaux sociaux (LinkedIn, Instagram, X, TikTok) — email froid ajouté en instruction supplémentaire (STRATEGISTE_EMAIL_INSTRUCTION), même technique que STRATEGISTE_MODE_INSTRUCTION, sans toucher au fichier crew-comm."
  - "2026-08-06 : le squelette de plan de redacteur.py n'accepte que les verbes 'Publier'/'Programmer'/'Planifier' (VERBES_ACTION, extraction non paramétrable de l'extérieur) — l'email réutilise ces mêmes verbes plutôt que de dupliquer l'extraction avec un 'Envoyer' supplémentaire, au prix d'un phrasé légèrement étrange ('Planifier l'email...'). Une seule logique d'extraction, zéro divergence avec crew-comm."
  - "2026-08-06 : pas de nouvelle logique Go — le graphe et les routeurs restent identiques, seul le contenu des prompts change. Pas de nouveaux tests unitaires en conséquence (rien de nouveau à tester en isolation), vérifié par une vraie passe E2E à la place."
---

**Quoi** : `community-management-agency` sait maintenant traiter des demandes de cold
email / outreach en plus des réseaux sociaux, avec séquences à plusieurs touches.

**Vérifié en réel** : dispatch d'une vraie demande de séquence cold email, le stratège a
correctement choisi "email froid (prospection écrite), 3 touches" avec une vraie
structure J0/J+3/J+7, le rédacteur a produit 3 emails complets (objet + corps) avec tout
inconnu marqué `[À COMPLÉTER]` (prénom, mode d'exercice, lien de rendez-vous, signature)
plutôt qu'inventé, et le plan de publication a été extrait correctement par la logique
existante de `crew-comm` sans aucune modification.

**Pièges** : aucun nouveau côté agent. Trouvé en vérifiant, côté kern-ui (pas ce commit) :
le panneau de détail plein écran (`role="dialog"` `aria-modal`) bloque les boutons
Valider/Refuser qui vivent ailleurs dans la page tant qu'il n'est pas fermé — comportement
de modal correct, pas un bug, mais à garder en tête en démo (fermer le panneau avant de
décider).
