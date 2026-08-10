---
id: 0026
feature: comm-x-channel
branch: feature/comm-x-channel
status: done
files:
  - skills/community-management-agency/agent_cli.py
  - skills/community-management-agency/SKILL.md
tests:
  - skills/community-management-agency/agent_cli_test.py
decisions:
  - "2026-08-06 : X est le deuxième canal avec un vrai connecteur (après Telegram). Instagram et TikTok restent en mode « propose, l'humain publie » — contrainte de plateforme réelle, pas un choix arbitraire : leurs API n'acceptent aucune des deux un post texte seul, toutes deux exigent une image/vidéo. Documenté comme piste future (kern-image, ou skill tiers type Higgsfield/GPT-image), non construit ce tour."
  - "2026-08-06 : signature OAuth 1.0a écrite à la main (hmac/hashlib/base64/urllib, stdlib) plutôt qu'une dépendance — l'API v2 de X exige ce schéma de signature pour publier en contexte utilisateur, et c'est le seul endpoint appelé par ce skill."
  - "2026-08-06 : limite de 280 caractères vérifiée AVANT l'appel réseau (RuntimeError explicite) plutôt que laissée à un 400 de l'API — un échec clair et attribuable plutôt qu'une erreur HTTP à interpréter."
  - "2026-08-06 : demandé explicitement par l'utilisateur — pas de vérification E2E réelle pour ce tour (pas de compte développeur X disponible), seulement suites unitaire et d'intégration (mock de urllib.request.urlopen)."
  - "2026-08-06 : détection de plateforme (X_PLATFORM_RE) exige un mot entier \"X\" (pas de correspondance à l'intérieur d'un autre mot) — contrairement à Telegram, la lettre seule aurait explosé en faux positifs sur n'importe quel texte français."
---

**Quoi** : `community-management-agency` peut publier réellement sur X en plus de
Telegram. Instagram/TikTok restent volontairement en brouillon (contrainte de plateforme,
pas un manque de temps) et sont documentés comme dépendant d'une future brique de
génération d'image.

**Vérifié** : `pytest` (23 tests, tous verts) — détection de plateforme (y compris le
faux-positif potentiel sur la lettre isolée), signature OAuth 1.0a de la requête,
dépassement de la limite de 280 caractères, garde-fou G2 conservé pour tout autre canal.
Pas de vérification E2E réelle (décision explicite, pas de compte développeur X
disponible ce tour). `go build`/`go test ./...` verts (aucune logique Go modifiée).

**Pièges** : trouvé en écrivant les tests, pas en le montrant en vrai cette fois — le
premier essai de `X_PLATFORM_RE` cherchait "plateforme(s)" en minuscules strictes sans
`re.IGNORECASE`, alors que le stratège écrit "Plateforme(s)" avec majuscule. Corrigé avec
un flag scopé `(?i:plateforme\(s\))` plutôt qu'un `re.IGNORECASE` global sur toute la
regex, pour garder `\bX\b` sensible à la casse (sinon la lettre minuscule "x" aurait
matché n'importe quel mot français qui la contient).
