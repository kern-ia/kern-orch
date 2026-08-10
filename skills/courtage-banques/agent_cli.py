#!/usr/bin/env python3
"""KERN_AGENT_CLI adapter for the courtage-banques graph
(examples/courtage-banques.yaml).

Same subprocess-per-node-invocation protocol as
skills/community-management-agency/agent_cli.py (see that file's docstring for the wire
shape) — this file follows the exact same conventions.

Besoin #4 de l'agence de courtage (specs.md) : "Un agent conversationnel interne interrogé
en langage naturel (« Quelle banque accepte un prêt hypothécaire sur un bien en SCI avec un
emprunteur de 74 ans ? »)" — dispatché librement (/courtage-banques <question>), pas
rattaché à un dossier précis, contrairement à courtage-extraction. Un seul nœud : interroge
kern-memory (EPIC-13 phase 1, /api/v1/memory/query) puis synthétise une réponse avec
claude -p, à partir UNIQUEMENT des extraits retournés — jamais des connaissances générales
du modèle sur le crédit immobilier (même discipline anti-invention que partout ailleurs).
"""
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request

CLAUDE_TIMEOUT_S = 180

# Vide = repli sur le défaut local (kern-memory tourne sur la même machine en dev/démo),
# configuré = ce qui est réellement joignable — même patron que send_telegram/send_x et
# ocr_image_mistral côté courtage-extraction, sauf qu'il n'y a pas de "moteur local de
# repli" ici : sans kern-memory joignable, il n'y a rien à interroger, donc erreur claire
# plutôt qu'une réponse inventée.
KERN_MEMORY_URL = os.environ.get("KERN_MEMORY_URL", "http://127.0.0.1:7080")
KERN_MEMORY_TOKEN = os.environ.get("KERN_MEMORY_TOKEN", "")

QUERY_LIMIT = 5

RAG_PROMPT = """Tu es un assistant interne qui répond aux questions sur les critères
d'octroi des banques partenaires, à partir UNIQUEMENT des extraits de mémoire fournis
ci-dessous. Ne réponds JAMAIS à partir de connaissances générales sur le crédit
immobilier — seulement à partir de ce qui est écrit dans les extraits. Si aucun extrait
pertinent n'est fourni, ou si les extraits ne répondent pas clairement à la question,
dis-le explicitement ("Aucune information disponible sur ce point dans la mémoire
actuelle.") au lieu d'inventer ou de généraliser.

Cite la source (l'identifiant entre crochets, ex. [a1b2c3]) pour chaque affirmation."""


def emit_result(output: dict) -> None:
    print(json.dumps({"type": "result", "output": output}), flush=True)


def emit_error(message: str) -> None:
    print(json.dumps({"type": "error", "message": message}), flush=True)


def run_claude(prompt: str) -> str:
    args = ["claude", "-p", prompt, "--output-format", "text"]
    result = subprocess.run(
        args, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=CLAUDE_TIMEOUT_S
    )
    if result.returncode != 0:
        raise RuntimeError(f"claude exited {result.returncode}: {result.stderr[:500]}")
    return result.stdout.strip()


def query_memory(question: str, limit: int = QUERY_LIMIT) -> list:
    payload = json.dumps({"text": question, "limit": limit}).encode()
    req = urllib.request.Request(
        f"{KERN_MEMORY_URL}/api/v1/memory/query",
        data=payload,
        method="POST",
        headers={"Content-Type": "application/json"},
    )
    if KERN_MEMORY_TOKEN:
        req.add_header("Authorization", f"Bearer {KERN_MEMORY_TOKEN}")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read())
    except (urllib.error.URLError, OSError) as e:
        raise RuntimeError(f"kern-memory injoignable ({KERN_MEMORY_URL}) : {e}") from e


def format_recalls(recalls: list) -> str:
    if not recalls:
        return "(aucun extrait pertinent trouvé dans la mémoire)"
    lines = []
    for r in recalls:
        mem = r["memory"]
        lines.append(f"[{mem['id']} | similarité {r['similarity']:.2f}] {mem['text']}")
    return "\n".join(lines)


def run_reponse(data: dict) -> dict:
    question = (data.get("message") or "").strip()
    if not question:
        raise RuntimeError("message manquant : aucune question posée.")

    recalls = query_memory(question)
    prompt = (
        f"{RAG_PROMPT}\n\n--- QUESTION ---\n{question}\n\n"
        f"--- EXTRAITS DE MÉMOIRE ---\n{format_recalls(recalls)}"
    )
    answer = run_claude(prompt)
    return {"reponse": answer, "display:reponse": answer}


NODE_HANDLERS = {
    "reponse": run_reponse,
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

    emit_result(output)


if __name__ == "__main__":
    main()
