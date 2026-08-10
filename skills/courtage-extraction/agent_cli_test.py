"""Pure-logic tests for agent_cli.py (courtage-extraction) — OCR engine selection,
per-page chunking (real Tesseract/PyMuPDF calls, no mocks needed for the local path),
JSON-fence stripping, and the Mistral OCR HTTP call (mocked, same pattern as
community-management-agency's send_telegram/send_x tests: no real network in tests).

Run: /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 -m pytest
     skills/courtage-extraction/agent_cli_test.py -v
"""
import io
import json
import sys
from pathlib import Path
from unittest.mock import MagicMock, patch

import fitz
from PIL import Image, ImageDraw, ImageFont

sys.path.insert(0, str(Path(__file__).parent))

import agent_cli as m  # noqa: E402

try:
    _TEST_FONT = ImageFont.truetype("/System/Library/Fonts/Helvetica.ttc", 36)
except OSError:
    _TEST_FONT = ImageFont.load_default()


def make_pdf(tmp_path, text_pages=(), image_pages=()) -> Path:
    """Builds a real PDF: text_pages get real embedded text (no OCR needed), image_pages
    get a rendered PNG of text burned into pixels (no text layer — forces OCR). A real
    scalable font at a real reading size, not the default bitmap font, so the OCR
    assertions below test the pipeline's correctness, not a synthetic-font artifact."""
    doc = fitz.open()
    for text in text_pages:
        page = doc.new_page()
        page.insert_text((72, 72), text)
    for text in image_pages:
        img = Image.new("RGB", (900, 150), "white")
        ImageDraw.Draw(img).text((20, 50), text, fill="black", font=_TEST_FONT)
        buf = io.BytesIO()
        img.save(buf, format="PNG")
        page = doc.new_page()
        page.insert_image(fitz.Rect(0, 0, 900, 150), stream=buf.getvalue())
    path = tmp_path / "dossier.pdf"
    doc.save(str(path))
    doc.close()
    return path


def test_pick_ocr_engine_defaults_to_tesseract_without_a_mistral_key(monkeypatch):
    monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
    assert m.pick_ocr_engine() == "tesseract"


def test_pick_ocr_engine_uses_mistral_once_a_key_is_configured(monkeypatch):
    monkeypatch.setenv("MISTRAL_API_KEY", "sk-test")
    assert m.pick_ocr_engine() == "mistral"


def test_strip_json_fence_removes_markdown_fences():
    assert m.strip_json_fence('```json\n{"a": 1}\n```') == '{"a": 1}'


def test_strip_json_fence_leaves_bare_json_untouched():
    assert m.strip_json_fence('{"a": 1}') == '{"a": 1}'


def test_extract_text_from_pdf_uses_embedded_text_without_ocr(tmp_path, monkeypatch):
    monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
    path = make_pdf(tmp_path, text_pages=["Revenu net 2400 euros"])

    result = m.extract_text_from_pdf(str(path))

    assert "Revenu net 2400 euros" in result["text"]
    assert result["pages"] == 1
    assert result["pages_ocr"] == []


def test_extract_text_from_pdf_falls_back_to_ocr_on_a_page_with_no_text_layer(tmp_path, monkeypatch):
    monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
    path = make_pdf(tmp_path, image_pages=["Solde compte 850 euros"])

    result = m.extract_text_from_pdf(str(path))

    assert "850" in result["text"]
    assert result["pages_ocr"] == [1]


def test_extract_text_from_pdf_handles_mixed_pages_independently(tmp_path, monkeypatch):
    monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
    path = make_pdf(tmp_path, text_pages=["Page texte"], image_pages=["Page scannee"])

    result = m.extract_text_from_pdf(str(path))

    assert result["pages"] == 2
    assert result["pages_ocr"] == [2]
    assert "[page 1]" in result["text"] and "[page 2]" in result["text"]


def test_ocr_image_mistral_sends_the_configured_key_and_returns_extracted_text():
    fake_response = MagicMock()
    fake_response.read.return_value = json.dumps(
        {"pages": [{"markdown": "Texte reconnu par Mistral"}]}
    ).encode()
    fake_response.__enter__.return_value = fake_response
    fake_response.__exit__.return_value = False

    with patch("agent_cli.urllib.request.urlopen", return_value=fake_response) as urlopen:
        text = m.ocr_image_mistral(b"fake-image-bytes", "sk-test")

    assert text == "Texte reconnu par Mistral"
    sent_request = urlopen.call_args[0][0]
    assert sent_request.get_header("Authorization") == "Bearer sk-test"


def test_ocr_image_mistral_raises_clearly_on_http_error():
    with patch("agent_cli.urllib.request.urlopen", side_effect=OSError("boom")):
        try:
            m.ocr_image_mistral(b"fake-image-bytes", "sk-test")
            assert False, "expected a RuntimeError"
        except RuntimeError as e:
            assert "Mistral" in str(e)


def test_run_reception_errors_when_document_path_is_missing():
    try:
        m.run_reception({})
        assert False, "expected a RuntimeError"
    except RuntimeError as e:
        assert "document_path" in str(e)


def test_run_reception_errors_when_the_file_does_not_exist(tmp_path):
    try:
        m.run_reception({"document_path": str(tmp_path / "absent.pdf")})
        assert False, "expected a RuntimeError"
    except RuntimeError as e:
        assert "introuvable" in str(e)


def test_run_reception_accepts_an_existing_file(tmp_path):
    path = make_pdf(tmp_path, text_pages=["ok"])
    out = m.run_reception({"document_path": str(path)})
    assert out["document_path"] == str(path)


def test_run_reception_reads_the_path_from_the_dispatch_message(tmp_path):
    # Real dispatch only ever sets state["message"] (internal/cmd/serve.go,
    # mailbox.Nudge("message", text)) — "document_path" is not a key dispatch can set.
    path = make_pdf(tmp_path, text_pages=["ok"])
    out = m.run_reception({"message": str(path)})
    assert out["document_path"] == str(path)


def test_run_extraction_end_to_end_on_a_real_pdf(tmp_path, monkeypatch):
    monkeypatch.delenv("MISTRAL_API_KEY", raising=False)
    path = make_pdf(tmp_path, text_pages=["Salaire net 2400 euros"])

    out = m.run_extraction({"document_path": str(path)})

    assert "Salaire net 2400 euros" in out["extracted_text"]
    assert out["extraction_pages_ocr"] == []


def test_run_extraction_rejects_an_unsupported_format(tmp_path):
    path = tmp_path / "dossier.txt"
    path.write_text("pas un document supporté")

    try:
        m.run_extraction({"document_path": str(path)})
        assert False, "expected a RuntimeError"
    except RuntimeError as e:
        assert ".txt" in str(e)


def test_run_interpretation_parses_a_clean_json_response():
    fake_json = json.dumps({"revenus": [], "credits_en_cours": [], "incidents": [],
                             "reste_a_vivre": {"montant": None, "methode_calcul": "x", "statut": "y"},
                             "pieces_manquantes": []})
    with patch("agent_cli.run_claude", return_value=fake_json):
        out = m.run_interpretation({"masked_text": "texte masqué"})

    parsed = json.loads(out["interpretation_masked"])
    assert parsed["pieces_manquantes"] == []


def test_run_interpretation_strips_markdown_fences_before_validating():
    fake_json = "```json\n" + json.dumps({"revenus": [], "credits_en_cours": [], "incidents": [],
                                           "reste_a_vivre": {"montant": None, "methode_calcul": "x", "statut": "y"},
                                           "pieces_manquantes": []}) + "\n```"
    with patch("agent_cli.run_claude", return_value=fake_json):
        out = m.run_interpretation({"masked_text": "texte masqué"})

    json.loads(out["interpretation_masked"])  # does not raise


def test_run_interpretation_tolerates_trailing_prose_after_the_json_object():
    # Real bug found live (2026-08-06): claude -p sometimes answers with a fenced JSON
    # block followed by more text, even when told to answer with ONLY the JSON object.
    real_shape = (
        '```json\n{\n  "revenus": [],\n  "credits_en_cours": [],\n  "incidents": [],\n'
        '  "reste_a_vivre": {"montant": null, "methode_calcul": "x", "statut": "y"},\n'
        '  "pieces_manquantes": []\n}\n```\n\nCeci complète le mémorandum en cours.'
    )
    with patch("agent_cli.run_claude", return_value=real_shape):
        out = m.run_interpretation({"masked_text": "texte masqué"})

    parsed = json.loads(out["interpretation_masked"])
    assert parsed["pieces_manquantes"] == []


def test_run_interpretation_raises_clearly_on_invalid_json():
    with patch("agent_cli.run_claude", return_value="ceci n'est pas du JSON"):
        try:
            m.run_interpretation({"masked_text": "texte masqué"})
            assert False, "expected a RuntimeError"
        except RuntimeError as e:
            assert "JSON" in str(e)


def test_run_memo_prep_errors_when_notes_entretien_is_missing():
    # Anti-invention discipline: no client history drafted from nothing — the analyst
    # must nudge notes_entretien onto the run before approving confirm_extraction.
    try:
        m.run_memo_prep({"interpretation": "{}"})
        assert False, "expected a RuntimeError"
    except RuntimeError as e:
        assert "notes_entretien" in str(e)


def test_run_memo_prep_combines_the_dossier_and_the_interview_notes():
    out = m.run_memo_prep({
        "interpretation": '{"revenus": []}',
        "notes_entretien": "Client senior, souhaite un prêt viager hypothécaire.",
    })

    assert '{"revenus": []}' in out["memo_text"]
    assert "Client senior, souhaite un prêt viager hypothécaire." in out["memo_text"]


def test_run_redaction_memo_returns_the_claude_draft():
    with patch("agent_cli.run_claude", return_value="## Mémorandum\n\nDraft ici."):
        out = m.run_redaction_memo({"memo_masked_text": "contenu masqué"})

    assert out["memo_draft_masked"] == "## Mémorandum\n\nDraft ici."


def test_run_relance_prep_lists_the_missing_pieces_in_the_message():
    out = m.run_relance_prep({
        "document_path": "/tmp/dossier_dupont.pdf",
        "interpretation": json.dumps({
            "pieces_manquantes": ["avis d'imposition N-1", "3e bulletin de salaire"],
        }),
    })

    assert "avis d'imposition N-1" in out["message"]
    assert "3e bulletin de salaire" in out["message"]
    assert "dossier_dupont.pdf" in out["message"]


def test_run_relance_prep_never_invents_pieces_when_interpretation_is_unparseable():
    out = m.run_relance_prep({"document_path": "/tmp/x.pdf", "interpretation": "pas du JSON"})

    assert out["message"]  # still produces a safe, generic message
    assert "avis d'imposition" not in out["message"]  # nothing invented
