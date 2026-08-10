#!/bin/bash
# Same wrapper pattern as skills/courtage-extraction/run.sh — KERN_AGENT_CLI only carries
# a bare executable path, no arguments.
exec /Users/yoann/Developer/mon-orchestrateur-agents/.venv/bin/python3 \
  /Users/yoann/Developer/SERENIS_PROJET/Kern-Orch/skills/courtage-banques/agent_cli.py
