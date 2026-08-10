#!/usr/bin/env python3
"""Standalone Telegram document-reception listener for courtage-extraction (besoin #1,
canal d'ingestion, specs.md "Ingestion des documents").

kern-orch's dispatch API (POST /api/v1/dispatch) is pure request/response — nothing in
kern-orch polls or listens for anything. Receiving Telegram documents needs something that
DOES listen, which is why this is a separate long-running process, not a graph node: it
long-polls Telegram's getUpdates, and for each incoming document, downloads it locally and
calls kern-orch's existing dispatch endpoint — the same entry point a human typing in the
chat already uses. No new kern-orch machinery, no new graph, just a second real caller of
what already exists.

Unlike the internal relance notification (courtage-relance, which never contacts a client
directly — see agent_cli.py's run_relance_prep docstring), replying HERE to the chat that
just sent a document is the one Telegram flow that IS legitimate without a client contact
database: the sender messaged the bot first, so Telegram allows a reply in that same
conversation — the constraint that blocked besoin #3 (a bot cannot message a user who
never initiated contact) does not apply to a reply.

Run: python3 telegram_listener.py
Requires: TELEGRAM_BOT_TOKEN (same variable community-management-agency/agent_cli.py
reads), KERN_ORCH_URL, KERN_ORCH_TOKEN.
"""
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

TELEGRAM_BOT_TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "")
KERN_ORCH_URL = os.environ.get("KERN_ORCH_URL", "http://127.0.0.1:7070")
KERN_ORCH_TOKEN = os.environ.get("KERN_ORCH_TOKEN", "")
INBOX_DIR = os.environ.get(
    "COURTAGE_TELEGRAM_INBOX",
    "/Users/yoann/Developer/SERENIS_PROJET/Kern-Orch/skills/courtage-extraction/inbox",
)

POLL_TIMEOUT_S = 30
RETRY_DELAY_S = 5
SEEN_TTL_S = 600
SEEN_MAX = 500


class SeenFileIDs:
    """Bounded, time-limited dedup — a real bug found live (2026-08-07): the same document
    arrived as two separate update_ids ~10s apart (Telegram redelivery or a client-side
    retry, cause unconfirmed). A long-running listener must not assume single delivery
    from an external event source regardless of cause. Time-bounded (not a growing set)
    because this process runs forever; a legitimate re-send of the same file days later
    must still go through."""

    def __init__(self, ttl_s: int = SEEN_TTL_S, max_size: int = SEEN_MAX):
        self._ttl = ttl_s
        self._max = max_size
        self._seen: dict = {}

    def mark_if_new(self, file_id: str) -> bool:
        now = time.time()
        self._seen = {fid: ts for fid, ts in self._seen.items() if now - ts < self._ttl}
        if file_id in self._seen:
            return False
        if len(self._seen) >= self._max:
            oldest = min(self._seen, key=self._seen.get)
            del self._seen[oldest]
        self._seen[file_id] = now
        return True


def extract_document(update: dict):
    """Returns {"chat_id","file_id","file_name"} for an incoming document message, or
    None for any other update shape (text-only messages, edited messages, etc.) — the
    caller must be able to skip non-document updates without guessing."""
    message = update.get("message")
    if not message:
        return None
    document = message.get("document")
    if not document:
        return None
    return {
        "chat_id": message["chat"]["id"],
        "file_id": document["file_id"],
        "file_name": document.get("file_name") or "document",
    }


def get_updates(offset, token: str, timeout: int = POLL_TIMEOUT_S) -> list:
    params = {"timeout": timeout}
    if offset is not None:
        params["offset"] = offset
    url = f"https://api.telegram.org/bot{token}/getUpdates?{urllib.parse.urlencode(params)}"
    with urllib.request.urlopen(url, timeout=timeout + 10) as resp:
        body = json.loads(resp.read())
    return body.get("result", [])


def download_file(file_id: str, file_name: str, inbox_dir: str, token: str) -> str:
    """Downloads a Telegram file (getFile + the file's own download URL) into inbox_dir,
    returning the local path courtage-extraction's reception node will read."""
    try:
        with urllib.request.urlopen(
            f"https://api.telegram.org/bot{token}/getFile?file_id={file_id}", timeout=30
        ) as resp:
            meta = json.loads(resp.read())
        remote_path = meta["result"]["file_path"]
        with urllib.request.urlopen(
            f"https://api.telegram.org/file/bot{token}/{remote_path}", timeout=60
        ) as resp:
            content = resp.read()
    except (urllib.error.URLError, OSError) as e:
        raise RuntimeError(f"Telegram injoignable pendant le téléchargement : {e}") from e

    Path(inbox_dir).mkdir(parents=True, exist_ok=True)
    dest = Path(inbox_dir) / f"{int(time.time())}_{file_name}"
    dest.write_bytes(content)
    return str(dest)


def dispatch_extraction(file_path: str, orch_url: str, orch_token: str) -> dict:
    payload = json.dumps({"skill": "courtage-extraction", "text": file_path}).encode()
    req = urllib.request.Request(
        f"{orch_url}/api/v1/dispatch",
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json", "Authorization": f"Bearer {orch_token}"},
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read())
    except (urllib.error.URLError, OSError) as e:
        raise RuntimeError(f"kern-orch injoignable ({orch_url}) : {e}") from e


def _send_telegram_text(chat_id: int, text: str, token: str) -> None:
    payload = json.dumps({"chat_id": chat_id, "text": text}).encode()
    req = urllib.request.Request(
        f"https://api.telegram.org/bot{token}/sendMessage",
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=30):
        pass


def notify_receipt(chat_id: int, run_id: str, file_name: str, token: str) -> None:
    _send_telegram_text(
        chat_id,
        f"✅ Document reçu : {file_name}. Traitement lancé (dossier {run_id}).",
        token,
    )


def notify_error(chat_id: int, file_name: str, error: str, token: str) -> None:
    _send_telegram_text(
        chat_id,
        f"⚠️ Échec du traitement de {file_name} : {error}",
        token,
    )


def run_forever() -> None:
    if not TELEGRAM_BOT_TOKEN:
        print("telegram_listener: TELEGRAM_BOT_TOKEN manquant, arrêt.", file=sys.stderr)
        sys.exit(1)

    offset = None
    seen = SeenFileIDs()
    print(f"telegram_listener: écoute démarrée (inbox={INBOX_DIR})", file=sys.stderr)
    while True:
        try:
            updates = get_updates(offset, TELEGRAM_BOT_TOKEN)
        except (urllib.error.URLError, OSError) as e:
            print(f"telegram_listener: getUpdates a échoué ({e}), nouvelle tentative", file=sys.stderr)
            time.sleep(RETRY_DELAY_S)
            continue

        for update in updates:
            offset = update["update_id"] + 1
            doc = extract_document(update)
            if not doc:
                continue
            if not seen.mark_if_new(doc["file_id"]):
                print(f"telegram_listener: {doc['file_name']} déjà traité récemment, ignoré", file=sys.stderr)
                continue
            try:
                path = download_file(doc["file_id"], doc["file_name"], INBOX_DIR, TELEGRAM_BOT_TOKEN)
                result = dispatch_extraction(path, KERN_ORCH_URL, KERN_ORCH_TOKEN)
                run_id = result.get("run_id", "?")
                notify_receipt(doc["chat_id"], run_id, doc["file_name"], TELEGRAM_BOT_TOKEN)
                print(f"telegram_listener: {doc['file_name']} -> run {run_id}", file=sys.stderr)
            except RuntimeError as e:
                notify_error(doc["chat_id"], doc["file_name"], str(e), TELEGRAM_BOT_TOKEN)
                print(f"telegram_listener: {doc['file_name']} a échoué : {e}", file=sys.stderr)


if __name__ == "__main__":
    run_forever()
