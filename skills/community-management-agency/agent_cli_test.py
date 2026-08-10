"""Pure-logic tests for agent_cli.py — channel detection and the Telegram send path.

Run: /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 -m pytest
     skills/community-management-agency/agent_cli_test.py -v
"""
import sys
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))

import agent_cli as m  # noqa: E402


def test_extract_mode_reads_the_leading_marker():
    assert m.extract_mode("MODE: avis\nreste") == "avis"
    assert m.extract_mode("MODE: proposition\n...") == "proposition"


def test_extract_mode_defaults_to_proposition_when_missing():
    # Safe default: never skip the human approval gate on an unrecognized/absent marker.
    assert m.extract_mode("pas de marqueur") == "proposition"
    assert m.extract_mode("") == "proposition"


def test_strip_mode_line_removes_only_the_marker():
    assert m.strip_mode_line("MODE: avis\nLe reste du texte") == "Le reste du texte"


def test_telegram_platform_detected_from_the_brief():
    brief = "Angle : x\nPlateforme(s) : Telegram\nFormat : court"
    assert m.TELEGRAM_PLATFORM_RE.search(brief)


def test_telegram_platform_not_detected_for_other_channels():
    brief = "Angle : x\nPlateforme(s) : LinkedIn\nFormat : post"
    assert not m.TELEGRAM_PLATFORM_RE.search(brief)


def test_telegram_platform_detection_is_case_insensitive():
    assert m.TELEGRAM_PLATFORM_RE.search("plateforme(s) : telegram")


def test_telegram_platform_detected_through_markdown_bold():
    # Real bug, found live: strategiste writes "**Plateforme(s)** : Telegram" — the
    # asterisks between the label and the colon broke the original regex.
    assert m.TELEGRAM_PLATFORM_RE.search("**Plateforme(s)** : Telegram uniquement.")


def test_telegram_platform_detected_without_the_plural_s():
    # Real bug, found live: strategiste sometimes writes "Plateforme :" (singular), not
    # always "Plateforme(s) :" — the original regex required the literal "(s)".
    assert m.TELEGRAM_PLATFORM_RE.search("Plateforme : Telegram (seule pertinente ici).")


def test_send_telegram_returns_empty_when_unconfigured():
    with patch.dict("os.environ", {}, clear=True):
        assert m.send_telegram("hello") == ""


def test_send_telegram_posts_to_the_configured_chat():
    with patch.dict(
        "os.environ", {"TELEGRAM_BOT_TOKEN": "t", "TELEGRAM_CHAT_ID": "42"}
    ), patch("agent_cli.urllib.request.urlopen") as mock_open:
        mock_open.return_value.__enter__.return_value.read.return_value = (
            b'{"ok": true, "result": {"message_id": 7}}'
        )
        ref = m.send_telegram("hello")

    assert "7" in ref
    request = mock_open.call_args[0][0]
    assert "api.telegram.org/bott/sendMessage" in request.full_url


def test_send_telegram_raises_on_a_refused_message():
    with patch.dict(
        "os.environ", {"TELEGRAM_BOT_TOKEN": "t", "TELEGRAM_CHAT_ID": "42"}
    ), patch("agent_cli.urllib.request.urlopen") as mock_open:
        mock_open.return_value.__enter__.return_value.read.return_value = (
            b'{"ok": false, "description": "chat not found"}'
        )
        try:
            m.send_telegram("hello")
            assert False, "expected RuntimeError"
        except RuntimeError as e:
            assert "chat not found" in str(e)


def test_run_publieur_sends_for_real_on_telegram_when_approved_and_configured():
    data = {
        "decision:confirm_publication": "approve",
        "plan_propose": "- Publier sur Telegram le 10/08 : test",
        "brief_editorial": "Plateforme(s) : Telegram",
        "texte_redige": "Bonjour, ceci est un test.",
    }
    with patch.dict(
        "os.environ", {"TELEGRAM_BOT_TOKEN": "t", "TELEGRAM_CHAT_ID": "42"}
    ), patch("agent_cli.send_telegram", return_value="message_id 9") as mock_send:
        out = m.run_publieur(data)

    mock_send.assert_called_once_with("Bonjour, ceci est un test.")
    assert "Envoyé" in out["execution"]


def test_run_publieur_falls_back_to_g2_guard_on_telegram_without_credentials():
    data = {
        "decision:confirm_publication": "approve",
        "plan_propose": "- Publier sur Telegram le 10/08 : test",
        "brief_editorial": "Plateforme(s) : Telegram",
        "texte_redige": "Bonjour.",
    }
    with patch.dict("os.environ", {}, clear=True):
        out = m.run_publieur(data)

    assert "Aucun connecteur" in out["execution"]


def test_run_publieur_never_sends_telegram_when_refused():
    data = {
        "decision:confirm_publication": "refuse",
        "plan_propose": "- Publier sur Telegram le 10/08 : test",
        "brief_editorial": "Plateforme(s) : Telegram",
        "texte_redige": "Bonjour.",
    }
    with patch("agent_cli.send_telegram") as mock_send:
        out = m.run_publieur(data)

    mock_send.assert_not_called()
    assert "Refusé" in out["execution"]


def test_run_publieur_keeps_the_g2_guard_for_non_telegram_channels():
    data = {
        "decision:confirm_publication": "approve",
        "plan_propose": "- Publier sur LinkedIn le 10/08 : test",
        "brief_editorial": "Plateforme(s) : LinkedIn",
        "texte_redige": "Bonjour.",
    }
    with patch.dict(
        "os.environ", {"TELEGRAM_BOT_TOKEN": "t", "TELEGRAM_CHAT_ID": "42"}
    ), patch("agent_cli.send_telegram") as mock_send:
        out = m.run_publieur(data)

    mock_send.assert_not_called()
    assert "Aucun connecteur" in out["execution"]


# --- X / Twitter ---

X_ENV = {
    "X_API_KEY": "ck",
    "X_API_SECRET": "cs",
    "X_ACCESS_TOKEN": "tk",
    "X_ACCESS_TOKEN_SECRET": "ts",
}


def test_x_platform_detected_as_the_standalone_letter():
    assert m.X_PLATFORM_RE.search("**Plateforme(s)** : X")


def test_x_platform_detected_via_twitter():
    assert m.X_PLATFORM_RE.search("Plateforme(s) : Twitter")


def test_x_platform_detected_without_the_plural_s():
    assert m.X_PLATFORM_RE.search("Plateforme : X (seule pertinente ici).")


def test_x_platform_not_matched_inside_an_unrelated_word():
    # "X" must be a standalone token — must not fire on every French word containing an x.
    brief = "Plateforme(s) : LinkedIn (choix maximal pour ce public)"
    assert not m.X_PLATFORM_RE.search(brief)


def test_send_x_returns_empty_when_unconfigured():
    with patch.dict("os.environ", {}, clear=True):
        assert m.send_x("hello") == ""


def test_send_x_refuses_a_message_over_the_280_character_limit():
    with patch.dict("os.environ", X_ENV):
        try:
            m.send_x("x" * 281)
            assert False, "expected RuntimeError"
        except RuntimeError as e:
            assert "280" in str(e)


def test_send_x_signs_the_request_with_an_oauth1_header():
    with patch.dict("os.environ", X_ENV), patch(
        "agent_cli.urllib.request.urlopen"
    ) as mock_open:
        mock_open.return_value.__enter__.return_value.read.return_value = (
            b'{"data": {"id": "123"}}'
        )
        ref = m.send_x("hello world")

    assert "123" in ref
    request = mock_open.call_args[0][0]
    assert request.full_url == "https://api.twitter.com/2/tweets"
    assert request.get_header("Authorization").startswith("OAuth ")
    assert "oauth_signature" in request.get_header("Authorization")


def test_send_x_raises_with_the_api_error_body_on_refusal():
    err = m.urllib.error.HTTPError(
        "https://api.twitter.com/2/tweets", 401, "Unauthorized", {}, None
    )
    err.read = lambda: b'{"detail": "bad token"}'
    with patch.dict("os.environ", X_ENV), patch(
        "agent_cli.urllib.request.urlopen", side_effect=err
    ):
        try:
            m.send_x("hello")
            assert False, "expected RuntimeError"
        except RuntimeError as e:
            assert "bad token" in str(e)


def test_run_publieur_sends_for_real_on_x_when_approved_and_configured():
    data = {
        "decision:confirm_publication": "approve",
        "plan_propose": "- Publier sur X le 10/08 : test",
        "brief_editorial": "Plateforme(s) : X",
        "texte_redige": "Bonjour, ceci est un test.",
    }
    with patch.dict("os.environ", X_ENV), patch(
        "agent_cli.send_x", return_value="tweet_id 9"
    ) as mock_send:
        out = m.run_publieur(data)

    mock_send.assert_called_once_with("Bonjour, ceci est un test.")
    assert "Envoyé" in out["execution"]


def test_run_publieur_never_sends_x_when_refused():
    data = {
        "decision:confirm_publication": "refuse",
        "plan_propose": "- Publier sur X le 10/08 : test",
        "brief_editorial": "Plateforme(s) : X",
        "texte_redige": "Bonjour.",
    }
    with patch("agent_cli.send_x") as mock_send:
        out = m.run_publieur(data)

    mock_send.assert_not_called()
    assert "Refusé" in out["execution"]
