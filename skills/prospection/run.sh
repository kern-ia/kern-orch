#!/bin/bash
# KERN_AGENT_CLI only carries a bare executable path, no arguments — this wrapper is
# what lets it run agent_cli.py under the venv that has crew-crm's prompt-text
# dependencies (langchain_core, python-dotenv) installed. Same pattern as
# examples/wrap-agent-cli.sh from kern-exec's own wiring.
exec /Users/yoann/mon-orchestrateur-agents/.venv/bin/python3 \
  /Users/yoann/Developer/kern/Kern-Orch/skills/prospection/agent_cli.py
