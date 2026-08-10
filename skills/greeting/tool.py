#!/usr/bin/env python3
import json
import sys

req = json.load(sys.stdin)
name = req.get("input", {}).get("name", "?")
print(json.dumps({"label": "Salutation", "value": f"Bonjour, {name} !"}))
