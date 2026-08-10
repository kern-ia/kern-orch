#!/usr/bin/env python3
"""KERN_AGENT_CLI adapter for the prospection graph (examples/prospection.yaml).

One process per node invocation — agentrunner.Subprocess's protocol (see
Kern-Orch/internal/agentrunner/protocol.go): one JSON request on stdin
({"node_id","prompt","state"}), one JSON-lines event on stdout, the last "result" event
wins. No interactivity: kern-orch's own ApprovalNode is what pauses the graph for a human
decision between "commercial" and "approved" — this script never blocks waiting for input.

Shells out to the real `claude` CLI (Claude Code) for reasoning and MCP tool calls,
rather than a separate LLM API. Switched from an Ollama/Gemini-via-LangChain design
(see git history) after finding the demo's Gemini key has a free quota of ~20
requests/day on the only viable model — Claude Code uses the operator's own existing
authentication instead, already speaks MCP natively, and does its own tool-calling loop
internally, which is why this adapter needs no LangChain machinery at all: `claude -p`
runs to completion and returns finished text on stdout.

Reuses crew-crm's already-written prompts (plain text constants, no LangChain coupling)
as a library — Kern is the demo, crew-crm a proven source of prompt engineering, not a
parallel system running alongside it.

`graph.State` wire shape (Kern-Orch/internal/graph/state.go, MarshalJSON): the state
object on the wire is {"step":N,"frozen":N,"data":{...},"zones":{...}} — every value an
agent node reads or writes lives under "data", not at the top level.
"""
import json
import os
import re
import subprocess
import sys
import tempfile
from datetime import date
from pathlib import Path

CREW_CRM = Path("/Users/yoann/mon-orchestrateur-agents/agents-locaux/crew-crm")
sys.path.insert(0, str(CREW_CRM))

# Prompt text and pure string helpers only — no LangChain, no model wiring imported.
from agents.commercial import PROMPT as COMMERCIAL_PROMPT  # noqa: E402
from agents.commercial import _marquer_inventions  # noqa: E402
from agents.expert import PROMPT as EXPERT_PROMPT  # noqa: E402
from agents.secretaire import PROMPT as SECRETAIRE_PROMPT  # noqa: E402

# The CRM's read-only tools this pipeline's secretaire is allowed to call — mirrors
# mcp_client.py's own NOMS_LECTURE/suffix heuristic, spelled out explicitly here since
# Claude Code's --allowedTools takes literal tool names (or a trailing-* wildcard), not
# the same suffix-matching logic.
READ_TOOLS = [
    "mcp__crm__ping",
    "mcp__crm__dashboard_stats",
    "mcp__crm__calendar_view",
    "mcp__crm__companies_list",
    "mcp__crm__companies_get",
    "mcp__crm__contacts_list",
    "mcp__crm__opportunities_board",
    "mcp__crm__scrape_jobs_list",
    "mcp__crm__scrape_job_get",
    "mcp__crm__quotes_list",
    "mcp__crm__quotes_get",
]

CLAUDE_TIMEOUT_S = 180


def emit_result(output: dict) -> None:
    print(json.dumps({"type": "result", "output": output}), flush=True)


def emit_error(message: str) -> None:
    print(json.dumps({"type": "error", "message": message}), flush=True)


def mcp_config_path() -> str:
    """Writes a one-off MCP config file for `claude --mcp-config` — the CRM's
    streamable_http endpoint, credentials from this process's own environment (never
    baked into the file's contents beyond this single run's temp file, deleted after)."""
    url = os.environ["CRM_MCP_URL"]
    token = os.environ["CRM_MCP_TOKEN"]
    config = {
        "mcpServers": {
            "crm": {"type": "http", "url": url, "headers": {"Authorization": f"Bearer {token}"}}
        }
    }
    fd, path = tempfile.mkstemp(suffix=".json", prefix="kern-crm-mcp-")
    with os.fdopen(fd, "w") as f:
        json.dump(config, f)
    return path


def run_claude(prompt: str, allowed_tools: list[str], use_mcp: bool) -> str:
    """Runs `claude -p` to completion and returns its plain-text answer. `capture_output`
    keeps whatever claude prints on ITS OWN stdout/stderr entirely inside `result` — it
    never touches this process's own stdout, so (unlike the previous LangChain-based
    adapter) there is no risk of polluting the JSON-lines protocol this script itself
    must speak on its own stdout."""
    args = ["claude", "-p", prompt, "--output-format", "text"]
    mcp_path = None
    if use_mcp:
        mcp_path = mcp_config_path()
        args += ["--mcp-config", mcp_path, "--strict-mcp-config"]
    if allowed_tools:
        args += ["--allowedTools", *allowed_tools]
    try:
        result = subprocess.run(
            args, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=CLAUDE_TIMEOUT_S
        )
    finally:
        if mcp_path:
            os.unlink(mcp_path)
    if result.returncode != 0:
        raise RuntimeError(f"claude exited {result.returncode}: {result.stderr[:500]}")
    return result.stdout.strip()


def run_secretaire(data: dict) -> dict:
    message = data.get("message", "")
    prompt = f"{SECRETAIRE_PROMPT}\n\n--- DEMANDE ---\n{message}"
    fiche = run_claude(prompt, READ_TOOLS, use_mcp=True)
    return {"lead_context": fiche}


def run_commercial(data: dict) -> dict:
    fiche = data.get("lead_context", "")
    fiche_bloc = f"--- FICHE PRÉPARÉE PAR LA SECRÉTAIRE ---\n{fiche}" if fiche else ""
    system = COMMERCIAL_PROMPT.format(
        aujourd_hui=date.today().strftime("%A %d %B %Y"), playbook="", fiche=fiche_bloc
    )
    # _filtrer_actions (below) only recognizes a line as an action if it starts with a
    # bullet or number — reinforced explicitly here, found necessary when this adapter
    # still called Gemini directly (it wrote plain paragraphs instead of bullets).
    prompt = (
        f"{system}\n\nFORMAT OBLIGATOIRE pour la section PLAN D'ACTION CRM : chaque "
        "action sur sa propre ligne, précédée d'un tiret \"- \" — jamais un paragraphe."
        "\n\nPrépare un plan d'action pour ce lead."
    )
    content = run_claude(prompt, ["WebSearch"], use_mcp=False)

    plan = extract_plan(content)
    return {"plan_propose": plan, "compte_rendu_commercial": content}


def extract_plan(content: str) -> str:
    """Pulls every bulleted line out of the PLAN D'ACTION CRM section. Unlike
    crew-crm's own _filtrer_actions (which additionally requires each line to *start*
    with a bare imperative verb from a fixed list), this keeps conditional or hedged
    actions too ("Si X existe, le créer", "Ne pas modifier Y tant que...") — found
    necessary by testing for real: Claude correctly hedges an action when the lead is
    missing data, and the stricter filter silently dropped every single line as a
    result, leaving an empty plan despite a well-formed CRM section. The human
    approval gate is the safety net for an over-broad extraction here, not a verb
    whitelist."""
    m = re.search(r"plan\s+d['']action\s+crm\s*:?", content, re.IGNORECASE)
    if not m:
        return ""
    section = re.split(r"\n(?:---|###|\*\*Message)", content[m.end():], maxsplit=1)[0]

    lignes = []
    for ligne in section.splitlines():
        bullet = re.match(r"^\s*(?:[-*]|\d+[.)])\s+(.+)", ligne)
        if bullet:
            lignes.append(bullet.group(1).strip())
        elif lignes and ligne.strip():
            lignes.append(ligne.strip())
    plan = "\n".join(f"- {ligne}" for ligne in lignes).strip()
    if plan:
        plan, _ = _marquer_inventions(plan, corpus="")
    return plan


def run_expert(data: dict) -> dict:
    plan = data.get("plan_propose", "")
    decision = data.get("decision:confirm", "")
    if decision != "approve":
        return {"execution": "Refusé par l'utilisateur : rien n'a été exécuté."}
    if not plan:
        return {"execution": "Aucun plan à exécuter."}

    member_id = os.environ.get("CRM_MEMBER_ID", "")
    prompt_expert = EXPERT_PROMPT
    if member_id:
        prompt_expert += f"\n- Quand un outil exige un memberId, utilise TOUJOURS : {member_id}"

    prompt = (
        f"{prompt_expert}\n\n--- PLAN VALIDÉ À EXÉCUTER ---\n{plan}\n\nExécute chaque "
        "action du plan en appelant les outils CRM nécessaires, une action à la fois, "
        "puis conclus par un compte-rendu action par action (✅/❌, outils appelés)."
    )
    report = run_claude(prompt, ["mcp__crm__*"], use_mcp=True)
    return {"execution": report}


NODE_HANDLERS = {
    "secretaire": run_secretaire,
    "commercial": run_commercial,
    "approved": run_expert,
}

# Which of a node's own output keys is its human-readable headline — what kern-ui's hive
# graph shows when someone clicks that node (Kern-UI/web/src/runs/HiveGraph.tsx reads
# state["display:<nodeId>"], a convention any node handler can opt into).
DISPLAY_KEYS = {
    "secretaire": "lead_context",
    "commercial": "compte_rendu_commercial",
    "approved": "execution",
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
