---
id: 0029
feature: courtage-extraction
branch: feature/courtage-extraction
status: done
files:
  - go.mod
  - internal/cmd/courtage_anon.go
  - internal/cmd/runtime.go
  - examples/courtage-extraction.yaml
  - skills/courtage-extraction/agent_cli.py
  - skills/courtage-extraction/run.sh
  - skills/courtage-extraction/SKILL.md
  - skills/specs.md
tests:
  - internal/cmd/courtage_anon_test.go
  - internal/cmd/courtage_graph_test.go
  - skills/courtage-extraction/agent_cli_test.py
decisions:
  - "2026-08-06 : kern-anon (github.com/YoLaub/PresidioGo) intégré comme vraie dépendance Go via `replace => ../Kern-Anon` (repo local, pas encore taggé/publié) — la brique existait depuis des mois avec 0% d'intégration réelle, ce besoin en est le premier vrai consommateur."
  - "2026-08-06 : masquage PII par jeton séquentiel (<IBAN_1>, <EMAIL_1>, <PII_1> en repli) via un opérateur Custom, PAS le Deanonymize positionnel natif de kern-anon — une sortie JSON reformulée par Claude ne préserve pas les offsets de caractères d'origine sur lesquels Deanonymize repose ; la substitution de chaîne sur une carte jeton→original est robuste à n'importe quelle réécriture du texte entre les deux."
  - "2026-08-06 : gap découvert en construisant le graphe — POST /api/v1/dispatch ne porte qu'un champ texte, aucun mécanisme de fichier n'existe. Décision utilisateur : ingestion multi-canal visée (UI, Telegram, dossier), mais SEUL le canal chemin/dossier est câblé dans cette passe (le texte du chat EST le chemin) — upload UI et réception Telegram consignés comme besoins séparés dans specs.md, pas des détails de ce besoin-ci."
  - "2026-08-06 : node 'extraction' priorise le texte de la couche PDF native (fitz.Page.get_text()) et n'appelle l'OCR (local ou cloud) QUE sur une page sans couche texte — sobriété réelle, pas seulement une optimisation : rien n'est transmis à un moteur OCR tiers si ce n'est pas nécessaire."
  - "2026-08-06 : environnement de départ vérifié AVANT de coder — ni Tesseract ni les libs Python (pytesseract/pymupdf/pillow) n'étaient installés, ni de clé Mistral dans .env. Tesseract + libs installés réellement cette session (brew + pip) sur demande explicite de l'utilisateur ; le chemin Mistral reste non vérifié en conditions réelles faute de clé."
  - "2026-08-06 : kern-anon ne détecte que des entités à motif fixe (IBAN, téléphone FR, email, NIR, SIREN/SIRET, carte bancaire...) — aucun moteur NLP/ONNX branché, donc un nom propre en texte libre n'est PAS masqué. Limite documentée explicitement dans SKILL.md plutôt que laissée implicite."
---

**Quoi** : premier besoin de l'agence de courtage (rachat de crédit) — extraction
documentaire d'un dossier PDF/image vers un JSON structuré (revenus, crédits en cours,
incidents, reste à vivre, pièces manquantes), avec masquage PII systématique avant que
tout modèle de raisonnement ne voie le texte, et validation humaine du résultat final.
Pipeline : `reception → extraction → masquage_pii → interpretation → demasquage_pii →
confirm_extraction`.

**Vérifié en réel** : `go test ./...` vert (tout le monorepo, kern-anon compilé et lié),
`pytest skills/courtage-extraction/agent_cli_test.py` vert (18 tests, aucun mock sur le
chemin local — vrai Tesseract, vrai PyMuPDF sur de vrais PDF générés). Deux vrais dispatch
HTTP de bout en bout contre un `kern-orch` relancé avec le bon `KERN_AGENT_CLI` :
1. PDF avec couche texte (IBAN + email réels) → aucun appel OCR, masquage confirmé
   (`<IBAN_1>`/`<EMAIL_1>` dans `masked_text`, valeurs réelles absentes), `claude -p` a
   produit un JSON honnête (statuts "à vérifier", a explicitement noté les coordonnées
   "non exploitables car masquées" sans jamais inventer de valeur), approbation ->
   `extraction_validee`, run `done`.
2. Image PNG sans couche texte (relevé simulé) → OCR Tesseract réel déclenché, texte
   correctement reconnu, refus -> `extraction_a_corriger`, run `done`.

Chemin Mistral OCR : code écrit et testé (appel HTTP mocké, en-tête `Authorization`
vérifié), **jamais appelé en vrai** — aucune clé `MISTRAL_API_KEY` disponible cette
session. À vérifier dès qu'une clé existe.

**Pièges** : le gap d'ingestion documentaire (aucune route de fichier dans l'API dispatch)
n'a été découvert qu'en construisant le graphe, après le cadrage OCR/format JSON/découpage
par page — un rappel générique que "cadrer avant de coder" peut quand même laisser passer
un maillon (ici : comment le document arrive physiquement), à vérifier explicitement la
prochaine fois qu'un besoin implique une entrée qui n'est pas du texte de chat.
