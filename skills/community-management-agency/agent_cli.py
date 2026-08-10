#!/usr/bin/env python3
"""KERN_AGENT_CLI adapter for the community-management-agency graph
(examples/community-management-agency.yaml).

One process per node invocation — agentrunner.Subprocess's protocol (see
Kern-Orch/internal/agentrunner/protocol.go): one JSON request on stdin
({"node_id","prompt","state"}), one JSON-lines event on stdout, the last "result" event
wins. No interactivity: kern-orch's own ApprovalNode is what pauses the graph for a human
decision at confirm_strategie/confirm_publication — this script never blocks on input.

Shells out to the real `claude` CLI (Claude Code), exactly like
skills/prospection/agent_cli.py — see that file's docstring for why (no separate LLM
API, no LangChain machinery, `claude -p` runs to completion and returns finished text).

Reuses crew-comm's already-written prompts (plain text constants, no LangChain coupling)
as a library — Kern is the demo, crew-comm a proven source of prompt engineering, not a
parallel system running alongside it. `_extraire_plan` (redacteur.py) is imported the
same way prospection imports `_marquer_inventions` from crew-crm: private by naming
convention, reused anyway, same precedent.

`graph.State` wire shape (Kern-Orch/internal/graph/state.go, MarshalJSON): the state
object on the wire is {"step":N,"frozen":N,"data":{...},"zones":{...}} — every value an
agent node reads or writes lives under "data", not at the top level.
"""
import base64
import hashlib
import hmac
import json
import os
import re
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import date
from pathlib import Path

CREW_COMM = Path("/Users/yoann/Developer/mon-orchestrateur-agents/agents-locaux/crew-comm")
sys.path.insert(0, str(CREW_COMM))

# Prompt text and pure string helpers only — no LangChain, no model wiring imported.
from agents.audience import PROMPT as AUDIENCE_PROMPT  # noqa: E402
from agents.strategiste import PROMPT as STRATEGISTE_PROMPT  # noqa: E402
from agents.redacteur import PROMPT as REDACTEUR_PROMPT  # noqa: E402
from agents.redacteur import RAPPEL_FORMAT as REDACTEUR_RAPPEL_FORMAT  # noqa: E402
from agents.redacteur import _extraire_plan  # noqa: E402

CLAUDE_TIMEOUT_S = 180

# 2026-08-05, décision : crew-comm n'a qu'un mode (le stratège propose toujours). Le
# double mode (avis sur une stratégie déjà fournie par l'utilisateur / proposition par le
# stratège) est nouveau pour ce skill — ajouté ici comme une instruction supplémentaire,
# pas en modifiant crew-comm/agents/strategiste.py, pour rester une réutilisation en
# bibliothèque plutôt qu'une divergence silencieuse entre les deux systèmes.
STRATEGISTE_MODE_INSTRUCTION = """Avant toute chose, détermine le MODE de ce tour et
commence ta réponse par UNE SEULE ligne, exactement sous cette forme :
"MODE: avis" si la demande de l'utilisateur contient déjà une stratégie complète (angle,
plateforme, ton, moment) qu'il te demande seulement de commenter — dans ce cas tu donnes
un avis court (5 lignes maximum : ce qui fonctionne, ce qui manque, ce que tu changerais)
et tu NE PRODUIS PAS de section "BRIEF ÉDITORIAL".
"MODE: proposition" dans tous les autres cas — tu proposes alors la stratégie toi-même,
selon les règles ci-dessous, en terminant par la section "BRIEF ÉDITORIAL" comme demandé.
"""

MODE_RE = re.compile(r"^\s*MODE\s*:\s*(avis|proposition)", re.IGNORECASE)

# 2026-08-06, décision : crew-comm ne connaît que les réseaux sociaux (le prompt du
# stratège liste explicitement "LinkedIn, Instagram, X, TikTok..."). L'email froid n'y
# figure pas du tout — ajouté ici en instruction supplémentaire, même technique que
# STRATEGISTE_MODE_INSTRUCTION, sans toucher à crew-comm/agents/strategiste.py.
STRATEGISTE_EMAIL_INSTRUCTION = """Une plateforme supplémentaire existe, en plus de celles
déjà citées : "email froid" (prospection écrite, outreach). Si elle est pertinente pour la
demande, tu peux la choisir comme plateforme — avec ses propres codes, différents de
LinkedIn : objet court et concret (jamais putaclic), corps de 3 à 6 phrases, un seul
call-to-action, aucun emoji, ton direct. Si une relance à plusieurs touches est pertinente,
propose un nombre de touches (2 à 4) espacées dans le temps plutôt qu'un email unique —
précise l'espacement (ex. "J+3", "J+7") dans le brief éditorial."""

# Le squelette de plan de redacteur.py (RAPPEL_FORMAT) n'accepte que les lignes commençant
# par "Publier"/"Programmer"/"Planifier" (VERBES_ACTION, crew-comm/agents/redacteur.py) —
# _extraire_plan filtre tout le reste. Plutôt que dupliquer cette extraction avec un
# quatrième verbe, l'email réutilise le même verbe "Publier" au prix d'une légère bizarrerie
# de phrasé ("Publier l'email...") : garde une seule logique d'extraction, zéro divergence
# avec crew-comm.
REDACTEUR_EMAIL_INSTRUCTION = """Si le brief éditorial indique la plateforme "email froid" :
- Rédige une ligne "Objet : ..." avant le corps du message.
- Corps court (3 à 6 phrases), un seul call-to-action, aucun lien de tracking inventé.
- Si le brief demande plusieurs touches, rédige chaque touche séparément ("Touche 1",
  "Touche 2"...), chacune avec son propre objet et corps.
- Le squelette de plan reste inchangé : chaque ligne commence quand même par "Publier",
  "Programmer" ou "Planifier" — ex. "Publier l'email à <destinataire> le <date> : <objet>"."""

# Telegram est le premier canal avec un vrai connecteur (voir send_telegram plus bas) —
# contrairement à email/réseaux sociaux, encore en mode "propose, l'humain publie". Le
# ton reste volontairement différent de LinkedIn : Telegram est un canal direct, pas un
# canal de représentation professionnelle.
STRATEGISTE_TELEGRAM_INSTRUCTION = """Une autre plateforme est disponible : "Telegram"
(message direct, canal court). Codes propres : ton informel et direct (moins de
storytelling professionnel que LinkedIn), message court (quelques phrases), *gras* et
_italique_ (formatage Telegram natif) plutôt que des mises en forme complexes, emoji
ponctuel toléré si le ton s'y prête. Pas d'objet (ce n'est pas un email)."""

REDACTEUR_TELEGRAM_INSTRUCTION = """Si le brief éditorial indique la plateforme
"Telegram" : pas de section "Objet", texte direct prêt à être envoyé tel quel (le
formatage *gras*/_italique_ Telegram est autorisé dans le corps). Le squelette de plan
reste inchangé : "Publier sur Telegram le <date> : <référence au texte>"."""

# Détecte Telegram dans la ligne "Plateforme(s) :" du brief éditorial du stratège — pas
# une analyse fine, un mot-clé suffit ici : le geste qui déclenche vraiment l'envoi reste
# la validation humaine de confirm_publication, cette détection ne fait que choisir QUEL
# connecteur essayer une fois l'accord donné, jamais si on publie ou non.
TELEGRAM_PLATFORM_RE = re.compile(r"plateforme(\(s\))?[*_\s]*:.*telegram", re.IGNORECASE)


def send_telegram(text: str) -> str:
    """Sends text to the configured chat via the Telegram Bot API. Returns "" (caller
    falls back to the G2 no-connector message) when unconfigured; raises RuntimeError on
    a real failure (network, or Telegram itself refusing the message) so the human sees
    an honest failure rather than a false "published"."""
    token = os.environ.get("TELEGRAM_BOT_TOKEN", "")
    chat_id = os.environ.get("TELEGRAM_CHAT_ID", "")
    if not token or not chat_id:
        return ""

    url = f"https://api.telegram.org/bot{token}/sendMessage"
    payload = json.dumps({"chat_id": chat_id, "text": text}).encode()
    req = urllib.request.Request(
        url, data=payload, headers={"Content-Type": "application/json"}
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            body = json.loads(resp.read())
    except urllib.error.URLError as e:
        raise RuntimeError(f"Telegram injoignable : {e}") from e

    if not body.get("ok"):
        raise RuntimeError(f"Telegram a refusé le message : {body.get('description', body)}")
    return f"message_id {body['result']['message_id']}"


# X (2026-08-06) : deuxième canal avec un vrai connecteur, et le seul des trois évoqués
# ce tour (Instagram/X/TikTok) où "publier du texte seul" correspond à une vraie API —
# Instagram Graph API et l'API de publication TikTok exigent toutes les deux une image ou
# une vidéo, aucune n'accepte un post texte nu. Ces deux-là restent donc en mode "propose,
# l'humain publie" (garde-fou G2) tant qu'aucune brique de génération d'image n'existe
# (piste notée : kern-image, ou un skill appelant un service tiers type Higgsfield /
# GPT-image — à construire séparément, pas dans ce commit).
STRATEGISTE_X_INSTRUCTION = """Si tu choisis "X" comme plateforme : c'est un texte court
(limite stricte de 280 caractères côté plateforme — une publication plus longue sera
rejetée), percutant, peu ou pas de hashtags. Un seul post pour l'instant, pas de thread
(fonctionnalité pas encore prise en charge par ce canal)."""

REDACTEUR_X_INSTRUCTION = """Si le brief éditorial indique la plateforme "X" : corps du
message strictement inférieur à 280 caractères (limite de la plateforme, non négociable —
compte les caractères), un seul post, pas de section "Objet". Le squelette de plan reste
inchangé : "Publier sur X le <date> : <référence au texte>"."""

X_PLATFORM_RE = re.compile(r"(?i:plateforme(\(s\))?)[*_\s]*:[^\n]*\b(X|[Tt]witter)\b")

# Newsletter/blog (2026-08-06) : format long, à l'opposé des canaux courts ci-dessus —
# aucun connecteur réel (pas de plateforme d'emailing ni de CMS branché), reste en mode
# "propose, l'humain publie" comme LinkedIn/Instagram/TikTok. Ajouté par instruction, même
# technique que les canaux précédents, sans toucher à crew-comm.
STRATEGISTE_NEWSLETTER_INSTRUCTION = """Une autre plateforme est disponible :
"newsletter/blog" (format long). Codes propres, à l'opposé des réseaux sociaux et de
l'email court : article structuré de 500 à 1500 mots, titre accrocheur mais honnête (pas
putaclic), introduction qui pose le problème du lecteur avant la solution, 2 à 4 sections
avec sous-titres, conclusion avec un appel à l'action clair. Pour un blog, garder les
mots-clés naturels dans le texte (pas de bourrage SEO artificiel) ; pour une newsletter,
prévoir en plus une ligne d'objet courte et concrète (ce que voit le lecteur dans sa boîte
de réception, distincte du titre de l'article)."""

REDACTEUR_NEWSLETTER_INSTRUCTION = """Si le brief éditorial indique la plateforme
"newsletter/blog" :
- Si newsletter : une ligne "Objet : ..." avant le titre de l'article.
- Titre de l'article, puis introduction, puis les sections annoncées par le brief
  (sous-titres avec "##"), puis une conclusion avec un appel à l'action.
- Longueur réelle 500 à 1500 mots — ne pas tronquer artificiellement, mais ne pas délayer
  non plus pour atteindre un volume.
- Le squelette de plan reste inchangé : "Publier l'article le <date> : <titre>"."""

X_TWEETS_URL = "https://api.twitter.com/2/tweets"
X_CHAR_LIMIT = 280


def _oauth1_header(method: str, url: str, consumer_key: str, consumer_secret: str,
                    token: str, token_secret: str) -> str:
    """Builds an OAuth 1.0a Authorization header (HMAC-SHA1) — the signing scheme the X
    API v2 posting endpoint requires for user-context requests. Hand-rolled with stdlib
    (hmac/hashlib/base64) rather than a new dependency: RFC 5849 is small and stable, and
    this is the only endpoint this skill calls."""
    oauth_params = {
        "oauth_consumer_key": consumer_key,
        "oauth_nonce": secrets.token_hex(16),
        "oauth_signature_method": "HMAC-SHA1",
        "oauth_timestamp": str(int(time.time())),
        "oauth_token": token,
        "oauth_version": "1.0",
    }
    param_string = "&".join(
        f"{urllib.parse.quote(k, safe='')}={urllib.parse.quote(v, safe='')}"
        for k, v in sorted(oauth_params.items())
    )
    base_string = "&".join([
        method.upper(),
        urllib.parse.quote(url, safe=""),
        urllib.parse.quote(param_string, safe=""),
    ])
    signing_key = (
        f"{urllib.parse.quote(consumer_secret, safe='')}"
        f"&{urllib.parse.quote(token_secret, safe='')}"
    )
    signature = base64.b64encode(
        hmac.new(signing_key.encode(), base_string.encode(), hashlib.sha1).digest()
    ).decode()
    oauth_params["oauth_signature"] = signature
    return "OAuth " + ", ".join(
        f'{urllib.parse.quote(k, safe="")}="{urllib.parse.quote(v, safe="")}"'
        for k, v in sorted(oauth_params.items())
    )


def send_x(text: str) -> str:
    """Posts a single tweet via the X API v2. Same contract as send_telegram: "" when
    unconfigured, RuntimeError on a real failure — including a message over the
    platform's own character limit, checked before the network call rather than left to
    a confusing 400 from the API."""
    consumer_key = os.environ.get("X_API_KEY", "")
    consumer_secret = os.environ.get("X_API_SECRET", "")
    token = os.environ.get("X_ACCESS_TOKEN", "")
    token_secret = os.environ.get("X_ACCESS_TOKEN_SECRET", "")
    if not (consumer_key and consumer_secret and token and token_secret):
        return ""

    if len(text) > X_CHAR_LIMIT:
        raise RuntimeError(
            f"texte trop long pour X ({len(text)} caractères, limite {X_CHAR_LIMIT}) — "
            "publication non exécutée."
        )

    auth_header = _oauth1_header(
        "POST", X_TWEETS_URL, consumer_key, consumer_secret, token, token_secret
    )
    payload = json.dumps({"text": text}).encode()
    req = urllib.request.Request(
        X_TWEETS_URL,
        data=payload,
        headers={"Content-Type": "application/json", "Authorization": auth_header},
    )
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            body = json.loads(resp.read())
    except urllib.error.HTTPError as e:
        detail = e.read().decode(errors="replace")
        raise RuntimeError(f"X a refusé le message : {detail}") from e
    except urllib.error.URLError as e:
        raise RuntimeError(f"X injoignable : {e}") from e

    tweet_id = body.get("data", {}).get("id")
    if not tweet_id:
        raise RuntimeError(f"X a répondu sans identifiant de tweet : {body}")
    return f"tweet_id {tweet_id}"


def emit_result(output: dict) -> None:
    print(json.dumps({"type": "result", "output": output}), flush=True)


def emit_error(message: str) -> None:
    print(json.dumps({"type": "error", "message": message}), flush=True)


def run_claude(prompt: str) -> str:
    """Runs `claude -p` to completion and returns its plain-text answer. No MCP/tool
    access needed for this graph's nodes yet — content drafting and review, not action
    on an external system (that's what confirm_publication gates)."""
    args = ["claude", "-p", prompt, "--output-format", "text"]
    result = subprocess.run(
        args, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=CLAUDE_TIMEOUT_S
    )
    if result.returncode != 0:
        raise RuntimeError(f"claude exited {result.returncode}: {result.stderr[:500]}")
    return result.stdout.strip()


def run_audience(data: dict) -> dict:
    message = data.get("message", "")
    prompt = f"{AUDIENCE_PROMPT.format(regle_cadrage='')}\n\n--- DEMANDE ---\n{message}"
    brief = run_claude(prompt)
    return {"audience_context": brief}


def extract_mode(content: str) -> str:
    """Reads the mandatory leading "MODE: avis|proposition" marker. Missing or
    unrecognized defaults to "proposition" — the safe default, since onStrategyMode
    (internal/cmd/comm_routers.go) routes anything but a literal "avis" to the human
    approval gate rather than skipping it."""
    m = MODE_RE.match(content or "")
    return m.group(1).lower() if m else "proposition"


def strip_mode_line(content: str) -> str:
    return MODE_RE.sub("", content or "", count=1).strip()


def run_strategiste(data: dict) -> dict:
    audience = data.get("audience_context", "")
    message = data.get("message", "")
    system = STRATEGISTE_PROMPT.format(
        aujourd_hui=date.today().strftime("%A %d %B %Y"),
        regle_cadrage="",
        playbook="",
        audience=audience,
    )
    prompt = (
        f"{STRATEGISTE_MODE_INSTRUCTION}\n\n{STRATEGISTE_EMAIL_INSTRUCTION}"
        f"\n\n{STRATEGISTE_TELEGRAM_INSTRUCTION}\n\n{STRATEGISTE_X_INSTRUCTION}"
        f"\n\n{STRATEGISTE_NEWSLETTER_INSTRUCTION}\n\n{system}"
        f"\n\n--- DEMANDE DE L'UTILISATEUR ---\n{message}"
    )
    content = run_claude(prompt)

    mode = extract_mode(content)
    reste = strip_mode_line(content)
    output = {"mode": mode}
    if mode == "avis":
        output["avis_strategie"] = reste
        # Sans BRIEF ÉDITORIAL produit en mode avis, le rédacteur retombe sur la
        # stratégie telle que l'utilisateur l'a formulée — jamais réinventée ici.
        output["brief_editorial"] = message
    else:
        output["brief_editorial"] = reste
    return output


def run_redacteur(data: dict) -> dict:
    brief = data.get("brief_editorial", "(aucun brief éditorial disponible)")
    system = REDACTEUR_PROMPT.format(
        aujourd_hui=date.today().strftime("%A %d %B %Y"),
        rappel_format=REDACTEUR_RAPPEL_FORMAT,
        playbook="",
        brief=brief,
    )
    prompt = (
        f"{REDACTEUR_EMAIL_INSTRUCTION}\n\n{REDACTEUR_TELEGRAM_INSTRUCTION}"
        f"\n\n{REDACTEUR_X_INSTRUCTION}\n\n{REDACTEUR_NEWSLETTER_INSTRUCTION}\n\n{system}"
    )
    content = run_claude(prompt)
    plan = _extraire_plan(content)
    return {"plan_propose": plan, "texte_redige": content}


def run_publieur(data: dict) -> dict:
    decision = data.get("decision:confirm_publication", "")
    plan = data.get("plan_propose", "")
    brief = data.get("brief_editorial", "")
    if decision != "approve":
        return {"execution": "Refusé par l'utilisateur : rien n'a été publié."}
    if not plan:
        return {"execution": "Aucun plan de publication à exécuter."}

    # Telegram : premier canal avec un vrai connecteur. La validation humaine ci-dessus
    # reste le seul geste qui déclenche l'envoi — pas de mode automatique qui la
    # court-circuiterait, ça viendra plus tard comme un choix explicite séparé.
    if TELEGRAM_PLATFORM_RE.search(brief):
        texte = data.get("texte_redige", "")
        if texte:
            try:
                ref = send_telegram(texte)
            except RuntimeError as e:
                return {"execution": f"⚠️ Échec de l'envoi Telegram : {e}\n\n{plan}"}
            if ref:
                return {"execution": f"✅ Envoyé sur Telegram ({ref}).\n\n{plan}"}
        # Pas de texte, ou send_telegram a renvoyé "" (pas configuré) : retombe sur le
        # garde-fou G2 ci-dessous, mêmes règles que les autres canaux.

    # X : même contrat que Telegram ci-dessus (validation humaine = seul déclencheur).
    if X_PLATFORM_RE.search(brief):
        texte = data.get("texte_redige", "")
        if texte:
            try:
                ref = send_x(texte)
            except RuntimeError as e:
                return {"execution": f"⚠️ Échec de l'envoi X : {e}\n\n{plan}"}
            if ref:
                return {"execution": f"✅ Envoyé sur X ({ref}).\n\n{plan}"}

    # G2 de crew-comm/agents/publieur.py, reproduit à l'identique : sans connecteur de
    # publication branché, on n'appelle même pas le modèle — c'est la seule façon sûre
    # d'empêcher un faux compte-rendu de publication.
    return {
        "execution": (
            "⚠️ Aucun connecteur de publication n'est branché : plan validé mais NON "
            f"exécuté.\n\n{plan}"
        )
    }


NODE_HANDLERS = {
    "audience": run_audience,
    "strategiste": run_strategiste,
    "redacteur": run_redacteur,
    "publieur": run_publieur,
}

# What kern-ui's hive graph shows when someone clicks that node (same convention as
# skills/prospection/agent_cli.py's DISPLAY_KEYS — state["display:<nodeId>"]).
DISPLAY_KEYS = {
    "audience": "audience_context",
    "strategiste": "brief_editorial",
    "redacteur": "texte_redige",
    "publieur": "execution",
}


def main() -> None:
    req = json.loads(sys.stdin.readline())
    node_id = req["node_id"]
    data = (req.get("state") or {}).get("data") or {}

    handler = NODE_HANDLERS.get(node_id)
    if handler is None:
        emit_error(f"unknown node_id: {node_id}")
        return
    try:
        output = handler(data)
    except Exception as e:
        emit_error(str(e))
        return

    headline_key = DISPLAY_KEYS.get(node_id)
    if headline_key and output.get(headline_key):
        output[f"display:{node_id}"] = output[headline_key]

    emit_result(output)


if __name__ == "__main__":
    main()
