---
name: courtage-banques
type: agent
description: Répond en langage naturel aux questions sur les critères d'octroi des banques partenaires, à partir de la mémoire kern-memory — jamais de connaissances générales inventées, toujours sourcé
graph: examples/courtage-banques.yaml
---

# courtage-banques

Besoin #4 de l'agence de courtage (rachat de crédit) — voir
`Kern-Orch/skills/specs.md`, section 2. Un agent conversationnel interne, dispatché
librement (pas rattaché à un dossier précis, contrairement à `courtage-extraction`).

Dispatché depuis le chat (`/courtage-banques <question en langage naturel>`) — le texte du
message EST la question, même mécanisme que les autres skills de ce dépôt
(`mailbox.Nudge("message", text)` côté kern-orch).

## Pipeline

Un seul nœud, aucune validation humaine (lecture seule, aucun effet externe réel) :

1. Interroge `kern-memory` (`POST /api/v1/memory/query`, EPIC-13 phase 1) avec la question.
2. Synthétise une réponse (`claude -p`) à partir UNIQUEMENT des extraits retournés — jamais
   des connaissances générales du modèle sur le crédit immobilier. Chaque affirmation doit
   citer sa source. Si rien de pertinent n'est trouvé, le dit explicitement au lieu
   d'inventer ou de généraliser.

## Configuration

| Variable | Rôle | Défaut |
|---|---|---|
| `KERN_MEMORY_URL` | Adresse de `kern-memory` | `http://127.0.0.1:7080` (déploiement local) |
| `KERN_MEMORY_TOKEN` | Jeton porteur si `kern-memory` en exige un | (aucun) |

Sans `kern-memory` joignable, le nœud échoue avec une erreur claire — jamais de réponse
inventée en repli (contrairement au patron OCR local/cloud de `courtage-extraction`, il
n'existe pas de "moteur de repli" ici : sans mémoire, il n'y a rien à interroger).

## Alimenter la mémoire

Ce skill est en LECTURE SEULE — il n'écrit jamais dans `kern-memory`. Les vrais critères
banques doivent être fournis par l'équipe AvelFinances et écrits séparément via
`POST /api/v1/memory/write` sur `kern-memory` (voir `kern-memory/README.md`). Aucun
critère bancaire n'est fabriqué par ce skill ni par Claude — seul ce qui a été réellement
écrit en mémoire peut être répondu.

## Reste à faire

- Base de connaissances vide en production tant que l'équipe n'a pas fourni de vrais
  critères — non bloquant pour le code, mais le skill ne répond utilement à rien avant ça.
- Pas de mécanisme de mise à jour/expiration des critères (ils "évoluent souvent" selon le
  besoin prospect original) — écriture manuelle uniquement pour l'instant.
