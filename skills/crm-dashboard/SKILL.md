---
name: crm-dashboard
type: tool
description: Affiche en direct la valeur du pipeline commercial du CRM.
command: ["/Users/yoann/mon-orchestrateur-agents/.venv/bin/python3", "skills/crm-dashboard/tool.py"]
---

# crm-dashboard

No params: reads `dashboard_stats` from the CRM's MCP server (`CRM_MCP_URL`,
`CRM_MCP_TOKEN` in kern-orch's own environment — see README) and reports the current
pipeline value.

**Demo wiring note**: `command` points at an external venv (`mon-orchestrateur-agents`)
that already has the `mcp` package installed — pragmatic for tonight's demo, not the
final shape. A proper kern-orch-owned Python environment for skill subprocesses is a
follow-up, not solved here.
