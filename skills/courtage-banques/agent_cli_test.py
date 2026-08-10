"""Pure-logic tests for agent_cli.py (courtage-banques) — kern-memory HTTP query (mocked,
same pattern as community-management-agency's send_telegram/send_x and
courtage-extraction's ocr_image_mistral: no real network in tests) and prompt assembly.

Run: /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 -m pytest
     skills/courtage-banques/agent_cli_test.py -v
"""
import json
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).parent))

import agent_cli as m  # noqa: E402


def fake_response(payload):
    resp = MagicMock()
    resp.read.return_value = json.dumps(payload).encode()
    resp.__enter__.return_value = resp
    resp.__exit__.return_value = False
    return resp


def test_query_memory_sends_the_question_and_returns_the_recalls():
    payload = [{"memory": {"id": "a", "text": "critère banque A"}, "similarity": 0.8}]
    with patch("agent_cli.urllib.request.urlopen", return_value=fake_response(payload)) as urlopen:
        got = m.query_memory("quelle banque accepte les SCI")

    assert got == payload
    sent_request = urlopen.call_args[0][0]
    sent_body = json.loads(sent_request.data)
    assert sent_body["text"] == "quelle banque accepte les SCI"


def test_query_memory_sends_the_bearer_token_when_configured(monkeypatch):
    monkeypatch.setattr(m, "KERN_MEMORY_TOKEN", "sk-test")
    with patch("agent_cli.urllib.request.urlopen", return_value=fake_response([])) as urlopen:
        m.query_memory("x")

    sent_request = urlopen.call_args[0][0]
    assert sent_request.get_header("Authorization") == "Bearer sk-test"


def test_query_memory_raises_clearly_when_kern_memory_is_unreachable():
    with patch("agent_cli.urllib.request.urlopen", side_effect=OSError("connection refused")):
        try:
            m.query_memory("x")
            assert False, "expected a RuntimeError"
        except RuntimeError as e:
            assert "kern-memory" in str(e)


def test_format_recalls_lists_each_memory_with_its_similarity():
    recalls = [
        {"memory": {"id": "a", "text": "critère banque A"}, "similarity": 0.83},
        {"memory": {"id": "b", "text": "critère banque B"}, "similarity": 0.41},
    ]
    out = m.format_recalls(recalls)

    assert "critère banque A" in out
    assert "0.83" in out
    assert "critère banque B" in out


def test_format_recalls_says_explicitly_when_nothing_was_found():
    out = m.format_recalls([])

    assert "aucun" in out.lower()


def test_run_reponse_errors_when_the_question_is_missing():
    try:
        m.run_reponse({})
        assert False, "expected a RuntimeError"
    except RuntimeError as e:
        assert "message" in str(e)


def test_run_reponse_queries_memory_and_synthesizes_an_answer():
    recalls = [{"memory": {"id": "a", "text": "La banque Alpha accepte les SCI"}, "similarity": 0.9}]
    with patch("agent_cli.query_memory", return_value=recalls), \
         patch("agent_cli.run_claude", return_value="La banque Alpha accepte les SCI [a]."):
        out = m.run_reponse({"message": "quelle banque accepte les SCI ?"})

    assert out["reponse"] == "La banque Alpha accepte les SCI [a]."


def test_run_reponse_passes_the_recalls_into_the_prompt():
    recalls = [{"memory": {"id": "a", "text": "critère unique et identifiable"}, "similarity": 0.9}]
    with patch("agent_cli.query_memory", return_value=recalls), \
         patch("agent_cli.run_claude") as run_claude:
        run_claude.return_value = "réponse"
        m.run_reponse({"message": "une question"})

    sent_prompt = run_claude.call_args[0][0]
    assert "critère unique et identifiable" in sent_prompt
    assert "une question" in sent_prompt
