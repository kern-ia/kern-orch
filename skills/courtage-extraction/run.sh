#!/bin/bash
# Same wrapper pattern as skills/community-management-agency/run.sh: KERN_AGENT_CLI only
# carries a bare executable path, no arguments — this is what lets it run under the venv
# that has pytesseract/PyMuPDF (and, once configured, the Mistral OCR HTTP call) available.
exec /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 \
  /Users/yoann/Developer/SERENIS_PROJET/Kern-Orch/skills/courtage-extraction/agent_cli.py
