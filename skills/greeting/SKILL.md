---
name: greeting
type: tool
description: Envoie un message d'accueil personnalisé.
command: ["python3", "skills/greeting/tool.py"]
params:
  - name: name
    type: string
    required: true
---

# greeting

Demonstrates the tool invocation contract: reads `{"input": {"name": "..."}}` on stdin,
answers `{"label": "...", "value": "..."}` on stdout.
