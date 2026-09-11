"""Scenario AgentAdapter wrapping ScriptedPlanVerificationAgent.

Install langwatch-scenario and set SCENARIO_LIVE=1 to exercise the optional suite.
"""

from __future__ import annotations

from typing import Any

from assertions import Trace
from scripted_agent import ScriptedPlanVerificationAgent


def last_user_text(messages: list[Any]) -> str:
    for msg in reversed(messages):
        if isinstance(msg, dict):
            role = msg.get("role")
            content = msg.get("content")
        else:
            role = getattr(msg, "role", None)
            content = getattr(msg, "content", None)
        if role == "user":
            if isinstance(content, str):
                return content
            if isinstance(content, list):
                parts = []
                for part in content:
                    if isinstance(part, dict) and part.get("type") == "text":
                        parts.append(part.get("text") or "")
                    elif isinstance(part, str):
                        parts.append(part)
                return "\n".join(parts)
            return str(content or "")
    return ""


class MulticaPlanVerificationAdapter:
    """Duck-typed Scenario AgentAdapter (role AGENT)."""

    name = "MulticaPlanVerification"
    role = "Agent"  # Scenario AgentRole.AGENT value; overridden when Scenario is imported

    def __init__(self, trace: Trace | None = None) -> None:
        self.trace = trace or Trace()
        self._inner = ScriptedPlanVerificationAgent(self.trace)

    async def call(self, input: Any) -> str:
        messages = getattr(input, "messages", None) or input.get("messages")  # type: ignore[union-attr]
        text = last_user_text(list(messages or []))
        return self._inner.call_sync(text)
