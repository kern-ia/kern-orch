#!/usr/bin/env python3
"""Espace widget: a live read from the CRM's own MCP server (dashboard_stats).

Self-contained on purpose — no dependency on any other project's code, only the
standard `mcp` SDK and the CRM's own MCP endpoint. Kern's bricks depend only on
published contracts, never on another project's internals.

Reads CRM_MCP_URL / CRM_MCP_TOKEN from the environment (inherited from whatever process
starts kern-orch — see skills/crm-dashboard/SKILL.md).
"""
import asyncio
import json
import os
import sys

from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client


async def fetch() -> dict:
    url = os.environ["CRM_MCP_URL"]
    token = os.environ["CRM_MCP_TOKEN"]
    async with streamablehttp_client(url, headers={"Authorization": f"Bearer {token}"}) as (r, w, _):
        async with ClientSession(r, w) as session:
            await session.initialize()
            result = await session.call_tool("dashboard_stats", {})
            text = "".join(getattr(c, "text", "") for c in result.content)
            return json.loads(text)


def main() -> None:
    json.load(sys.stdin)  # {"input": {...}} — no params needed for this widget
    try:
        stats = asyncio.run(fetch())
        pipeline_euros = stats.get("pipelineCents", 0) / 100
        print(json.dumps({"label": "Pipeline commercial", "value": f"{pipeline_euros:.0f} €"}))
    except Exception as e:
        print(json.dumps({"error": f"CRM injoignable : {e}"}))


if __name__ == "__main__":
    main()
