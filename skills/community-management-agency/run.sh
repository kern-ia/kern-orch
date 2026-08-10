#!/bin/bash
# KERN_AGENT_CLI only carries a bare executable path, no arguments — this wrapper is
# what lets it run agent_cli.py under the venv that has crew-comm's prompt-text
# dependencies (langchain_core, langchain_ollama) installed. Same pattern as
# skills/prospection/run.sh.
exec /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 \
  /Users/yoann/Developer/SERENIS_PROJET/Kern-Orch/skills/community-management-agency/agent_cli.py
