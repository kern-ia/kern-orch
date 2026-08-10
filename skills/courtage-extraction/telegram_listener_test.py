"""Pure-logic tests for telegram_listener.py — update parsing, file download, and dispatch
calls (all HTTP mocked, same pattern as agent_cli_test.py's send_telegram/ocr_image_mistral
tests: no real network in tests).

Run: /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 -m pytest
     skills/courtage-extraction/telegram_listener_test.py -v
"""
import json
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).parent))

import telegram_listener as m  # noqa: E402


def fake_response(payload_or_bytes, is_json=True):
    resp = MagicMock()
    resp.read.return_value = json.dumps(payload_or_bytes).encode() if is_json else payload_or_bytes
    resp.__enter__.return_value = resp
    resp.__exit__.return_value = False
    return resp


def test_extract_document_reads_file_id_chat_id_and_name():
    update = {
        "update_id": 42,
        "message": {
            "chat": {"id": 123},
            "document": {"file_id": "AgAD", "file_name": "avis_imposition.pdf"},
        },
    }
    doc = m.extract_document(update)

    assert doc == {"chat_id": 123, "file_id": "AgAD", "file_name": "avis_imposition.pdf"}


def test_extract_document_returns_none_when_the_message_has_no_document():
    update = {"update_id": 1, "message": {"chat": {"id": 1}, "text": "bonjour"}}

    assert m.extract_document(update) is None


def test_extract_document_returns_none_for_a_non_message_update():
    update = {"update_id": 1, "edited_message": {"chat": {"id": 1}}}

    assert m.extract_document(update) is None


def test_extract_document_defaults_a_missing_file_name():
    update = {"update_id": 1, "message": {"chat": {"id": 1}, "document": {"file_id": "x"}}}

    doc = m.extract_document(update)

    assert doc["file_name"] == "document"


def test_get_updates_sends_the_offset_and_returns_the_result_list():
    payload = {"ok": True, "result": [{"update_id": 5}]}
    with patch("telegram_listener.urllib.request.urlopen", return_value=fake_response(payload)) as urlopen:
        got = m.get_updates(offset=5, token="tok")

    assert got == [{"update_id": 5}]
    sent_url = urlopen.call_args[0][0]
    assert "offset=5" in (sent_url if isinstance(sent_url, str) else sent_url.full_url)


def test_download_file_saves_bytes_under_the_inbox_dir(tmp_path):
    get_file_payload = {"ok": True, "result": {"file_path": "documents/file_1.pdf"}}
    file_bytes = b"%PDF-1.4 fake content"

    responses = [fake_response(get_file_payload), fake_response(file_bytes, is_json=False)]
    with patch("telegram_listener.urllib.request.urlopen", side_effect=responses):
        path = m.download_file("AgAD", "avis.pdf", str(tmp_path), token="tok")

    assert Path(path).exists()
    assert Path(path).read_bytes() == file_bytes
    assert Path(path).parent == tmp_path


def test_download_file_raises_clearly_when_telegram_is_unreachable(tmp_path):
    with patch("telegram_listener.urllib.request.urlopen", side_effect=OSError("boom")):
        try:
            m.download_file("AgAD", "avis.pdf", str(tmp_path), token="tok")
            assert False, "expected a RuntimeError"
        except RuntimeError as e:
            assert "Telegram" in str(e)


def test_dispatch_extraction_posts_the_local_path_as_the_message():
    payload = {"kind": "run", "run_id": "abc123"}
    with patch("telegram_listener.urllib.request.urlopen", return_value=fake_response(payload)) as urlopen:
        got = m.dispatch_extraction("/tmp/inbox/avis.pdf", orch_url="http://x", orch_token="t")

    assert got == payload
    sent_request = urlopen.call_args[0][0]
    sent_body = json.loads(sent_request.data)
    assert sent_body == {"skill": "courtage-extraction", "text": "/tmp/inbox/avis.pdf"}
    assert sent_request.get_header("Authorization") == "Bearer t"


def test_notify_receipt_sends_a_message_to_the_sending_chat():
    with patch("telegram_listener.urllib.request.urlopen", return_value=fake_response({"ok": True})) as urlopen:
        m.notify_receipt(chat_id=123, run_id="abc123", file_name="avis.pdf", token="tok")

    sent_request = urlopen.call_args[0][0]
    sent_body = json.loads(sent_request.data)
    assert sent_body["chat_id"] == 123
    assert "abc123" in sent_body["text"]
    assert "avis.pdf" in sent_body["text"]


def test_seen_file_ids_is_idempotent_across_repeated_updates():
    # Real bug found live (2026-08-07): the same document arrived as two separate
    # update_ids ~10s apart (Telegram redelivery or a client-side retry — cause
    # unconfirmed, but external event sources double-delivering is common enough that the
    # consumer must not assume single delivery regardless of cause).
    seen = m.SeenFileIDs()

    assert seen.mark_if_new("AgAD1") is True
    assert seen.mark_if_new("AgAD1") is False
    assert seen.mark_if_new("AgAD2") is True


def test_notify_error_sends_a_clear_failure_message():
    with patch("telegram_listener.urllib.request.urlopen", return_value=fake_response({"ok": True})) as urlopen:
        m.notify_error(chat_id=123, file_name="avis.pdf", error="kern-orch injoignable", token="tok")

    sent_request = urlopen.call_args[0][0]
    sent_body = json.loads(sent_request.data)
    assert sent_body["chat_id"] == 123
    assert "kern-orch injoignable" in sent_body["text"]
