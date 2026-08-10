#!/usr/bin/env python3
import json
import sys
from datetime import datetime

json.load(sys.stdin)
print(json.dumps({"label": "Battement", "value": datetime.now().strftime("%H:%M:%S")}))
