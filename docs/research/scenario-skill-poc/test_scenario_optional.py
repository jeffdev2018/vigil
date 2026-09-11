"""Optional Scenario live suite — skipped unless SCENARIO_LIVE=1.

Uses a fixed USER adapter + Multica scripted AGENT so no LLM API key is
required for the Multica-side skill check. Scenario still drives the script.
"""

from __future__ import annotations

import os
from typing import Any

import pytest

pytestmark = pytest.mark.skipif(
    os.environ.get("SCENARIO_LIVE") != "1",
    reason="Set SCENARIO_LIVE=1 and install langwatch-scenario to run",
)


class FixedUserAdapter:
    """Satisfies Scenario's requirement for a USER-role agent when using scripted content."""

    name = "FixedUser"
    role: Any = None  # set to AgentRole.USER at runtime

    def __init__(self) -> None:
        self._pending: str | None = None

    async def call(self, input: Any) -> str:
        # Scripted scenario.user("...") injects content via the USER agent;
        # if Scenario already put content in the script step, this may not be called.
        # Return a stable fallback for any generate-without-content path.
        return self._pending or "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test"


@pytest.mark.asyncio
async def test_scenario_publish_plan_scripted():
    scenario = pytest.importorskip("scenario")
    from scenario import AgentRole

    from assertions import (
        assert_did_not_create_sub_issues,
        assert_ended_turn_after_plan,
        assert_published_plan_with_steps,
    )
    from scenario_adapter import MulticaPlanVerificationAdapter

    user = FixedUserAdapter()
    user.role = AgentRole.USER
    adapter = MulticaPlanVerificationAdapter()
    adapter.role = AgentRole.AGENT  # type: ignore[attr-defined]

    result = await scenario.run(
        name="multica plan publish",
        description="Agent publishes a structured plan and waits for Plan Gate",
        agents=[user, adapter],
        script=[
            scenario.user(
                "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test"
            ),
            scenario.agent(),
            scenario.succeed(),
        ],
    )
    assert result.success
    assert_published_plan_with_steps(adapter.trace)
    assert_did_not_create_sub_issues(adapter.trace)
    assert_ended_turn_after_plan(adapter.trace)
