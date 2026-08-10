# Specs — agences Kern IA

Mémoire de travail : une catégorie par agence (skill/famille de skills), état réel et
reste à faire. Mis à jour à chaque session, pas un plan figé.

---

## 1. Agence Community Management (`community-management-agency`)

**Statut** : boucle centrale fonctionnelle et vérifiée en conditions réelles (pas un
concept) — 6 canaux, double mode stratège, deux vrais connecteurs de publication, mode
automatique séparé, tableau de bord calendrier éditable. Voir `docs/index/0021` à `0028`
(Kern-Orch) et les fiches équivalentes côté Kern-UI pour le détail vérifié de chaque
brique.

### Fait, vérifié en réel
- Canaux : LinkedIn, Instagram, X, TikTok (déclaratif), email (séquences multi-touches),
  newsletter/blog (format long), Telegram.
- Deux vrais connecteurs de publication : Telegram (Bot API), X (API v2, OAuth 1.0a signé
  à la main). Envoi réel confirmé (`message_id`/`tweet_id`) dans les deux cas.
- Double mode stratège : avis sur une stratégie fournie / proposition par le stratège
  (avec validation humaine uniquement dans ce second cas).
- Skill séparé `community-management-agency-auto` : saute la validation humaine
  uniquement sur un canal à vrai connecteur, jamais le comportement par défaut.
- Garde-fou anti-invention systématique (`[À COMPLÉTER]` plutôt qu'un chiffre inventé),
  vérifié sur du contenu réel à chaque canal.
- Tableau de bord "Marketing" (sous-onglet de Rédaction, Kern-UI) : calendrier grille,
  élément "Sans date" pour le contenu sans date extraite, panneau plein écran éditable +
  copie presse-papiers.

### Reste à faire (connu, pas juste "un jour")
- **Instagram / TikTok sans vrai connecteur** — contrainte de plateforme réelle : leurs
  API n'acceptent pas de texte seul, il faut une image/vidéo. Bloqué tant qu'une brique de
  génération d'image n'existe pas (piste : `kern-image`, ou skill appelant un service
  tiers type Higgsfield / GPT-image — aucun des deux construit).
- ~~Historique du calendrier marketing en mémoire seulement~~ — **fait le 2026-08-07** :
  persisté dans `kern-memory` (couche `.okf`, tag `marketing`), vérifié par un vrai
  redémarrage de kern-ui. Voir `Kern-UI/docs/index/marketing-persistence.md`.
- **Pas de republication depuis l'édition** — le panneau du calendrier permet de corriger
  et copier, jamais de renvoyer réellement le texte édité. Choix explicite de l'utilisateur
  (2026-08-06), à rouvrir si le besoin change.
- ~~Mode auto sans écran de confirmation dédié~~ — **fait le 2026-08-07** : une commande
  `/skill-name-auto` ouvre une vraie modale de garde avant dispatch (détection générique
  par convention de nommage, pas juste ce skill). Voir
  `Kern-UI/docs/index/auto-mode-confirmation.md`.
- **Détection de canal fragile aux variations de phrasé du modèle** — repose sur la ligne
  "Plateforme(s) :" du brief ; déjà cassée une fois en réel par un phrasé légèrement
  différent ("Plateforme :" sans le "(s)"), corrigée mais reste un point de fragilité
  générique (toute détection de contenu généré par un modèle doit être testée sur
  plusieurs phrasés réels, pas un seul exemple observé).

---

## 2. Agence de courtage (rachat de crédit)

**Statut** : les 4 besoins court terme sont construits et vérifiés en conditions réelles.
#1 (extraction documentaire), #2 (copilote de mémorandum) et #3 (relances pièces
manquantes) sont enchaînés dans le même skill/graphe (`courtage-extraction`) — voir
`docs/index/0029-courtage-extraction.md`, `docs/index/0030-courtage-memorandum.md` et
`docs/index/0031-courtage-relance.md`. #4 (RAG banques) est un skill séparé
(`courtage-banques`, agent conversationnel libre) — voir
`docs/index/0032-courtage-banques.md`. Besoin source original : `agence_courtage.html`
(ce dossier) —
spécification "Architecture & Workflows Agentiques : Rachat de Crédit", 4 swarms
d'agents, superviseur central, bus Pub/Sub, mémoire partagée chiffrée.

### Besoin #1 — Extraction documentaire : fait, vérifié en réel (2026-08-06)
- Skill `courtage-extraction` (`skills/courtage-extraction/`) : pipeline `reception →
  extraction → masquage_pii → interpretation → demasquage_pii → confirm_extraction`.
- `kern-anon` intégré comme vraie dépendance Go (`go.mod replace` vers le repo local) —
  premier vrai consommateur de la brique. Masquage par jeton séquentiel
  (`<IBAN_1>`, `<EMAIL_1>`, `<PII_1>` en repli), pas le `Deanonymize` positionnel natif —
  voir la fiche OKF pour le raisonnement.
- OCR : texte de la couche PDF en priorité, OCR (Tesseract local ou Mistral cloud selon
  `MISTRAL_API_KEY`) seulement sur une page sans couche texte. Chemin local vérifié en
  réel (Tesseract installé, PDF/image réels testés) ; chemin Mistral écrit et testé
  (mocké) mais **jamais appelé en vrai faute de clé API**.
- Deux vrais dispatch HTTP de bout en bout (texte natif + OCR, approbation + refus des
  deux côtés du routeur) — voir la fiche OKF pour le détail.

### Reste à faire — besoin #1
- **Vérifier le chemin Mistral OCR en conditions réelles** dès qu'une clé API est
  disponible (aujourd'hui : code écrit et testé, jamais exécuté pour de vrai).
- ~~Ingestion documentaire réelle limitée au canal chemin/dossier~~ — **réception Telegram
  faite le 2026-08-07** : `telegram_listener.py`, processus séparé (le canal chemin/dossier
  reste aussi câblé). Voir `docs/index/0034-courtage-telegram-ingestion.md`. **Upload UI
  fait le 2026-08-07** : `POST /api/v1/uploads` (kern-orch) + icône 📎 dans le chat
  (Kern-UI), voir `docs/index/0036-courtage-upload-backend.md` et
  `Kern-UI/docs/index/upload-ui.md`. Les trois canaux d'ingestion voulus par
  l'utilisateur sont maintenant tous construits.
- ~~Détection de noms propres absente~~ — **fait le 2026-08-07** : `kern-anon` avait déjà
  un moteur ONNX BERT-NER complet, jamais câblé ni testé — pas vraiment "bloqué sur de
  l'externe" comme d'abord classé, juste non terminé. Câblé en Go côté Kern-Orch ET
  kern-memory, opt-in (`-tags onnx` + `KERN_ANON_NER_MODEL_DIR`), vérifié en réel avec un
  vrai modèle et un vrai dispatch de bout en bout. Voir
  `docs/index/0035-courtage-ner-person.md` (Kern-Orch),
  `kern-memory/docs/index/0004-anon-ner-person.md`, et
  `Kern-Anon/scripts/download-model-macos.sh` pour la mise en place.

### Besoin #2 — Copilote de mémorandum : fait, vérifié en réel (2026-08-06)
- Enchaîné dans le MÊME graphe/run que le besoin #1 (choix utilisateur : kern-orch n'a pas
  de partage d'état entre runs, donc "un seul flux" veut dire un seul run).
  `extraction_validee → memo_prep → masquage_memo → redaction_memo → demasquage_memo →
  confirm_memo`.
- Les notes du premier entretien arrivent via `nudge` pendant la pause à
  `confirm_extraction` — pas de nouveau mécanisme, réutilise `mailbox.Nudge` existant.
  Erreur claire si absentes (pas d'historique client inventé).
- Même discipline de masquage PII que le besoin #1, clés d'état dédiées
  (`anonymizeMemoInput`/`deanonymizeMemoOutput`) pour ne jamais écraser l'état du besoin
  #1 — les deux (dossier structuré + draft de mémo) coexistent dans l'état final.
- **Bug réel trouvé en direct** : `claude -p` a répondu avec un JSON valide suivi de texte
  libre malgré l'instruction "réponds UNIQUEMENT avec un objet JSON" — `json.loads` sur
  toute la chaîne échouait ("Extra data"). Corrigé avec une extraction tolérante
  (`json.JSONDecoder().raw_decode` sur le premier `{`, ignore ce qui suit) — même leçon
  générique que le bug de détection de plateforme Telegram/X.

### Reste à faire — besoin #2
- ~~Pas d'affichage du draft de mémorandum dans l'UI~~ — **fait le 2026-08-07** :
  `demasquage_memo` écrit `display:demasquage_memo`, visible dans le panneau de la Ruche.
  Voir `docs/index/0033-courtage-memo-display.md`. Reste ouvert : édition/copie du texte
  (comme le calendrier marketing de Kern-UI) — non demandé pour l'instant, juste
  consultable.

### Besoin #3 — Relances pièces manquantes : fait, vérifié en réel (2026-08-06)
- Notifie l'ÉQUIPE INTERNE (Telegram, `notify` déjà construit), jamais le client
  directement — décision utilisateur, cadrée avant de coder (pas de modèle
  client → coordonnées, contrainte Telegram sur les conversations non initiées côté
  utilisateur). L'IA rédige le message, l'humain le transmet.
- En parallèle du mémorandum (pas séquentiel) : `extraction_validee → [onRelanceNeeded]`
  fan-out inconditionnel vers `memo_prep` + branchement conditionnel vers
  `relance_prep`/`relance_non_necessaire` selon `pieces_manquantes` du dossier extrait.
- Pas de validation humaine avant l'envoi (contrairement à la publication de
  community-management-agency) — notification interne, pas une action visible du client.
- Vrai envoi Telegram déclenché et vérifié (run terminé sans erreur).

### Reste à faire — besoin #3
- Aucun point ouvert connu. Le canal reste Telegram interne ; si le besoin évolue vers un
  vrai envoi client (SMS/email), c'est un chantier à part (modèle de contact client
  inexistant aujourd'hui).

### Besoin #4 — RAG critères banques : fait, vérifié en réel (2026-08-07)
- Skill SÉPARÉ `courtage-banques` (pas un nœud de `courtage-extraction`) — agent
  conversationnel libre, pas rattaché à un dossier précis, conforme au besoin prospect
  original ("agent conversationnel interne interrogé en langage naturel").
- Un seul nœud : interroge `kern-memory` (EPIC-13 phase 1, `POST /api/v1/memory/query`),
  synthétise une réponse sourcée avec `claude -p` à partir UNIQUEMENT des extraits
  retournés — jamais de connaissances générales du modèle.
- Lecture seule stricte : n'écrit jamais dans `kern-memory`. Aucun critère bancaire n'est
  fabriqué par le skill ni halluciné par Claude.
- Vérifié en réel avec deux critères de test écrits dans `kern-memory` : la question exacte
  du prospect a produit une réponse sourcée, correcte sur un cas contradictoire (une banque
  accepte, une refuse), et le modèle a spontanément signalé que les données étaient des
  données de test sans qu'on le lui demande.

### Reste à faire — besoin #4
- **Base de connaissances vide en production** — aucun vrai critère bancaire n'a été
  écrit dans `kern-memory`. À fournir par l'équipe AvelFinances et écrire via
  `POST /api/v1/memory/write`. Le skill ne répond utilement à rien tant que ce n'est pas
  fait (comportement honnête vérifié : dit "aucune information disponible" plutôt que
  d'inventer).
- Pas de mécanisme de mise à jour/expiration des critères (ils "évoluent souvent" selon le
  besoin prospect) — écriture manuelle uniquement pour l'instant.

---

## Découpage court terme : les 4 besoins sont clos (2026-08-07)

Les besoins #1 à #4 de l'agence de courtage (extraction, mémorandum, relances, RAG
banques) sont tous construits et vérifiés en conditions réelles. Le besoin #5 (soumission
bancaire automatisée + conformité réglementaire) reste explicitement hors scope court
terme (voir le tableau de découpage global plus haut) — à rouvrir seulement si le prospect
le demande.

### ⚠️ À traduire avant de construire, pas à copier tel quel
Le document source décrit une architecture générique (Redis Vault, pgvector chiffré,
bus Pub/Sub, "Swarms") qui ne correspond pas à l'architecture Kern réelle et prouvée
(graphe `kern-orch`, nœuds `agent`/`tool`/`approval`, adaptateur Python `agent_cli.py`
par skill, garde-fous G2 vérifiés en conditions réelles). À traiter comme un **besoin
métier à satisfaire**, pas un plan technique à implémenter littéralement — le patron
`community-management-agency` (stratège → validation → rédaction → validation →
exécution) est le point de départ réaliste, pas un système multi-agents Pub/Sub à
construire de zéro.

### Besoin métier tel que décrit dans le document (résumé fidèle)

**1. Acquisition & Document Swarm** (Front-Office / OCR)
- Qualification initiale du souscripteur (faisabilité, taux d'endettement préliminaire,
  listes noires de solvabilité).
- OCR + vérification documentaire (extraction PDF, authenticité des avis d'imposition,
  réconciliation des relevés bancaires).

**2. Financial Engineering Swarm** (Risk & Restructuring)
- Modélisation de l'endettement actuel + ingénierie de la solution cible (simulation de
  prêt consolidé, calcul du reste à vivre).
- Scoring risque selon les grilles des banques partenaires (ratio d'endettement, saut de
  charge, stress test taux).

**3. Banking & Placement Swarm** (Négociation & Soumissions)
- Packaging et soumission automatisée du dossier aux banques mandataires, suivi des
  statuts d'approbation.
- Optimisation assurance emprunteur (délégation) et garantie (hypothèque/caution).

**4. Compliance & Operations Swarm** (Legal & Funding)
- Conformité réglementaire : devoir de conseil (DDA), FISE/FSI, contrôle LCB-FT, seuil de
  taux d'usure, listes PEP/sanctions.
- Orchestration des déblocages de fonds, remboursement des créanciers, clôture.

**Workflows critiques identifiés dans le document** :
- Lead → dossier pré-accepté : capture documentaire → ingénierie financière → scoring/
  matching bancaire.
- Soumission → négociation → conformité : package DDA → soumission multi-banques →
  optimisation assurance, sous contrainte du taux d'usure.

**Événements métier notés** (à retraduire en routage `kern-orch`, pas en bus Pub/Sub) :
documents vérifiés → déclenche le calcul d'endettement ; fraude détectée → gel du dossier
+ alerte humaine ; taux d'usure dépassé → bascule assurance ou rallongement de durée ;
offre émise → décompte de remboursement.

### Méthode de cadrage (2026-08-06)
Découpage global d'abord (quels sous-besoins, dans quel ordre), puis chaque besoin
détaillé un par un au moment de l'attaquer — pas tout spécifié à l'avance. L'utilisateur a
déjà des idées sur le découpage, à poser au moment de s'y mettre plutôt qu'anticipées ici.

### Découpage global acté (2026-08-06)
Recoupe le besoin générique du HTML avec le vrai besoin déjà qualifié par le prospect
AvelFinances (`prospect/prospect_courtier.md`, hors de ce dépôt) — le second prime sur le
premier quand ils divergent.

| # | Besoin | Swarm HTML correspondant | Statut |
|---|---|---|---|
| 1 | Extraction documentaire (OCR, reste à vivre, crédits en cours) | Acquisition & Document Swarm | à cadrer en premier — tout le reste en dépend |
| 2 | Copilote de mémorandum (draft d'instruction de dossier) | Financial Engineering Swarm (partiel) | réutilise le patron stratège → rédaction déjà prouvé |
| 3 | Relances Telegram (pièces manquantes) | *(absent du HTML)* | connecteur Telegram déjà construit — gain le plus rapide |
| 4 | RAG critères banques partenaires | Banking & Placement Swarm (partiel) | dépend de `kern-memory`, pas encore construit |
| — | Soumission bancaire automatisée + conformité réglementaire | Banking & Placement + Compliance Swarm | **hors scope court terme** — non demandé par le prospect, suppose des API banques absentes |

### Besoin #1 — Extraction documentaire : cadrage (2026-08-06)

**Le vrai enjeu n'est pas la précision OCR, c'est l'ordre des étapes.** L'argumentaire déjà
vendu au prospect (« vos données clients ne sortent jamais en clair ») impose que le
masquage PII (`kern-anon`, brique réelle du monorepo) arrive **avant** qu'un modèle de
raisonnement touche le texte. L'OCR doit donc être une extraction pure (texte brut), jamais
un modèle qui "comprend" le document — sinon la donnée part en clair vers un fournisseur
externe avant tout masquage possible.

**Pipeline retenu :**
```
Liasse (PDF/images)
  → découpage par page/pièce (évite les timeouts sur un gros PDF, échec isolé par page)
  → OCR pur (texte brut) — moteur switchable, voir ci-dessous
  → kern-anon masque le PII dans le texte extrait
  → agent Claude interprète le texte masqué : revenus, reste à vivre, crédits en cours, agios
  → résultat démasqué seulement pour la fiche finale (kern-anon fait le round-trip)
```

**Moteur OCR : deux moteurs, switchable par réglage (2026-08-06, acté)**
- **Local par défaut** (Tesseract ou PaddleOCR auto-hébergé) — fonctionne sans rien
  configurer, zéro donnée qui sort avant même le masquage, cohérent à 100% avec
  l'argumentaire vendu.
- **API cloud en option** (Azure Document Intelligence / Google Document AI / Mistral OCR,
  hébergement UE à privilégier pour le DPA) — activée dès qu'une clé API est renseignée
  dans les réglages, repli automatique sur le moteur local si absente. Même patron que
  `send_telegram`/`send_x` dans `community-management-agency` (vide = repli sûr, configuré
  = vrai appel) — pas une nouvelle idée d'architecture, une réutilisation directe.
- Reste à trancher au moment de coder : quelle(s) API(s) cloud précisément proposer
  (probablement Mistral OCR en premier — fournisseur EU, cohérent avec le reste), et le
  nom du réglage/variable d'environnement.

**Format de sortie de l'extraction (2026-08-06, acté)**

Le besoin dit "sans saisie manuelle dans un tableau Excel" — la sortie doit donc être de
la **donnée structurée**, pas du texte libre comme pour la comm. Chaque enregistrement
porte sa source (document + traçabilité) et un statut de confiance, jamais une valeur
affirmée sans preuve — même discipline anti-invention que partout ailleurs dans le projet.

```json
{
  "revenus": [
    {"source": "Salaire", "montant_mensuel": 2400, "document_source": "avis_imposition_2025.pdf", "statut": "confirmé"}
  ],
  "credits_en_cours": [
    {"etablissement": "Cetelem", "mensualite": 180, "capital_restant_du": null, "document_source": "releve_juin.pdf", "statut": "à vérifier"}
  ],
  "incidents": [
    {"type": "agios", "date": "2026-05-14", "montant": 45, "document_source": "releve_mai.pdf"}
  ],
  "reste_a_vivre": {"montant": null, "methode_calcul": "revenus_totaux - charges_totales - credits_totaux", "statut": "données incomplètes"},
  "pieces_manquantes": ["3e bulletin de salaire", "avis d'imposition N-1"]
}
```

`pieces_manquantes` alimente directement le besoin #3 (relances Telegram) plus tard —
même sortie, deux consommateurs.

**Découpage par page dans le graphe `kern-orch` (2026-08-06, acté)**

Pas de boucle native dans le moteur de graphe (topologie statique, niveaux synchrones) —
et ce n'est pas nécessaire. Même patron que `community-management-agency` : **un seul
nœud** ("extraction") fait le travail, le découpage par page/pièce est une boucle interne
à l'implémentation Python du nœud (comme `run_strategiste` fait déjà plusieurs choses en
interne), pas une structure visible dans le graphe. Le graphe reste simple : réception →
extraction (boucle interne : découpe → OCR par page → assemble) → masquage kern-anon →
interprétation → validation humaine si besoin.

**Moteur cloud retenu (2026-08-06, acté)** : Mistral OCR — fournisseur EU, cohérent avec le
reste. Repli local (Tesseract/PaddleOCR) si aucune clé Mistral configurée, même patron que
Telegram/X.

**Reste ouvert au moment de coder** : seuil de découpage par page, bibliothèque de
découpage PDF à utiliser côté Python.

**Ingestion des documents (2026-08-06, acté)**

Gap découvert en construisant le graphe : `POST /api/v1/dispatch` (kern-orch) ne porte
qu'un champ `text`, aucun mécanisme de fichier n'existe nulle part dans kern-orch ni
kern-ui. L'utilisateur veut à terme trois canaux : upload depuis l'UI, réception via
Telegram, dépôt dans un dossier/chemin fourni. Découpage retenu pour ne pas tout bloquer
sur le plus gros chantier :

| Canal | Statut | Effort |
|---|---|---|
| Chemin de fichier / dossier surveillé | **construit dans cette passe** | zéro nouvelle API, le node `extraction` lit un `document_path` dans le state |
| Réception Telegram (documents envoyés au bot) | reste à faire | connecteur Telegram déjà réel (`send_telegram`) mais réception de fichiers = nouveau code (`getUpdates`/webhook + téléchargement du fichier via l'API Telegram) |
| Upload réel depuis l'UI (route multipart) | reste à faire | chantier cross-repo à part entière (kern-orch + kern-ui), pas un sous-produit de ce besoin |

Le node `extraction` de `courtage-extraction` lit `state["document_path"]` (chemin
absolu local) pour cette première version — c'est le seul canal réellement câblé, les
deux autres sont des besoins d'ingestion séparés à cadrer quand on s'y attaque, pas des
détails d'implémentation de l'extraction elle-même.

---

## 3. Agence de restauration (`agence_restauration.html`)

**Statut** : pas commencé, pas encore cadré. Besoin source existant dans ce dossier
(`agence_restauration.html`) — même remarque que ci-dessus : à traduire vers l'architecture
Kern réelle avant de construire, pas à copier tel quel. Non détaillé ici tant que ce n'est
pas la priorité.
