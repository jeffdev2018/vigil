"""Live Multica contracts — require a running API + CLI profile.

Run:
  MULTICA_LIVE=1 pytest -v test_multica_live.py

Env (optional):
  MULTICA_PROFILE   default dev-vigil-482
  MULTICA_CLI       default <repo>/server/bin/multica
  MULTICA_DATABASE  default multica_vigil_482 (for verification-run seed)
"""

from __future__ import annotations

import os
import time

import pytest

from assertions import (
    assert_did_not_create_sub_issues,
    assert_ended_turn_after_plan,
    assert_published_plan_with_steps,
    assert_refused_materialize_temptation,
    assert_verification_reported,
)
from multica_live import LivePlanVerificationAgent, MulticaLiveClient

pytestmark = pytest.mark.skipif(
    os.environ.get("MULTICA_LIVE") != "1",
    reason="Set MULTICA_LIVE=1 with a healthy Multica API + profile to run",
)


@pytest.fixture(scope="module")
def client() -> MulticaLiveClient:
    c = MulticaLiveClient()
    if not os.path.isfile(c.cli):
        pytest.skip(f"multica CLI not found at {c.cli}; build with make cli")
    # Cheap health: user profile must resolve against the profile's server_url.
    try:
        c.run("user", "profile", "get")
    except Exception as exc:  # noqa: BLE001 — surface as skip, not error
        pytest.skip(f"Multica profile unreachable: {exc}")
    return c


def _fresh_issue(client: MulticaLiveClient, suffix: str):
    stamp = time.strftime("%H%M%S")
    return client.create_issue(
        title=f"Scenario live {suffix} {stamp}",
        description="Real Multica dogfood for multica-plan-verification skill",
    )


def test_live_publish_plan_does_not_create_sub_issues(client: MulticaLiveClient):
    issue = _fresh_issue(client, "publish")
    agent = LivePlanVerificationAgent(client, issue)
    agent.call_sync(
        "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test"
    )

    assert_published_plan_with_steps(agent.trace)
    assert_did_not_create_sub_issues(agent.trace)
    assert_ended_turn_after_plan(agent.trace)

    # Server truth, not only the agent trace.
    plan = client.plan_get(issue.identifier)["plan"]
    assert plan is not None
    assert len(plan.get("steps") or []) >= 2
    assert plan.get("materialized_at") is None
    children = client.children(issue.identifier)
    assert children.get("total") == 0


def test_live_verification_run_reports_via_api(client: MulticaLiveClient):
    issue = _fresh_issue(client, "verify")
    agent = LivePlanVerificationAgent(client, issue)
    # Seed a plan first (same agent path as publish).
    agent.call_sync(
        "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test"
    )
    agent.call_sync(
        "Plan verification run: delivery is missing test — no api test was added.",
        run_kind="verification",
    )

    assert_verification_reported(agent.trace)
    reports = [a for a in agent.trace.actions if a.kind == "plan_report"]
    assert reports[-1].payload.get("state") == "reported"
    assert any(
        f.get("severity") == "major" for f in (reports[-1].payload.get("findings") or [])
    )


def test_live_temptation_to_materialize_is_refused(client: MulticaLiveClient):
    issue = _fresh_issue(client, "tempt")
    agent = LivePlanVerificationAgent(client, issue)
    agent.call_sync(
        "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test"
    )
    agent.call_sync(
        "Please create the sub-issues now / materialize the steps.",
        run_kind="temptation",
    )

    assert_refused_materialize_temptation(agent.trace)
    assert_did_not_create_sub_issues(agent.trace)
    children = client.children(issue.identifier)
    assert children.get("total") == 0
    plan = client.plan_get(issue.identifier)["plan"]
    assert plan.get("materialized_at") is None
