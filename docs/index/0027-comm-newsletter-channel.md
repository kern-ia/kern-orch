---
id: 0027
feature: comm-newsletter-channel
branch: feature/comm-newsletter-channel
status: done
files:
  - skills/community-management-agency/agent_cli.py
  - skills/community-management-agency/SKILL.md
tests: []
decisions:
  - "2026-08-06 : newsletter/blog ajouté comme quatrième canal, format long (500-1500 mots) — pas de connecteur réel (aucune plateforme d'emailing ni de CMS branché), reste en mode 'propose, l'humain publie' comme LinkedIn/Instagram/TikTok."
  - "2026-08-06 : structure imposée par instruction — objet (si newsletter) + titre honnête + introduction qui pose le problème + 2-4 sections à sous-titres + conclusion avec appel à l'action, plutôt que de laisser le rédacteur improviser une forme longue."
  - "2026-08-06 : pas de nouvelle logique Go ni Python testable en isolation (contenu de prompt uniquement) — vérifié par une vraie passe E2E au lieu de tests unitaires, comme pour le canal email."
---

**Quoi** : `community-management-agency` peut rédiger un article de blog ou une
newsletter (format long, 500-1500 mots) en plus des canaux courts existants.

**Vérifié en réel** : dispatch d'une vraie demande d'article, le stratège a choisi
"newsletter/blog" et proposé une structure claire, le rédacteur a produit un article
complet — objet, titre honnête ("Pourquoi tester un POC ne remet pas en cause votre
méthode", pas de putaclic), introduction qui pose le problème, 4 sections à sous-titres,
conclusion avec CTA — avec tout inconnu (chiffres internes, date, lien de contact) marqué
`[À COMPLÉTER]` plutôt qu'inventé. Run complet jusqu'au bout, garde-fou G2 confirmé (pas
de connecteur, rien publié). `go build`/`go test ./...` verts.

**Pièges** : aucun.
