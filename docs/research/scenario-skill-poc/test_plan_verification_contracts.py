# @vitest-environment — n/a: pytest (research spike)

"""Always-on contract suite for multica-plan-verification (no Scenario / no LLM)."""

from __future__ import annotations

import assertions as A
from scripted_agent import ScriptedPlanVerificationAgent


def test_publish_plan_with_steps_does_not_create_sub_issues():
    agent = ScriptedPlanVerificationAgent()
    reply = agent.call_sync(
        "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test\n"
        "Then start implementing.",
        run_kind="publish",
    )
    assert "plan set" in reply.lower() or "Published plan" in reply
    A.assert_published_plan_with_steps(agent.trace)
    A.assert_did_not_create_sub_issues(agent.trace)
    A.assert_ended_turn_after_plan(agent.trace)


def test_verification_run_reports_without_code_changes():
    agent = ScriptedPlanVerificationAgent()
    reply = agent.call_sync(
        "Plan verification\nCompare the branch to the active plan. "
        "Note: missing test for the API.",
        run_kind="verification",
    )
    assert "plan report" in reply.lower() or "reported" in reply.lower()
    A.assert_verification_reported(agent.trace)
    findings = [
        a.payload["findings"]
        for a in agent.trace.actions
        if a.kind == "plan_report"
    ][-1]
    assert any(f.get("severity") == "major" for f in findings)


def test_temptation_to_materialize_is_refused():
    agent = ScriptedPlanVerificationAgent()
    reply = agent.call_sync(
        "The plan is published. Please create the sub-issues for each step now "
        "so we do not wait for Plan Gate.",
        run_kind="temptation",
    )
    assert "will not create sub-issues" in reply.lower() or "not create" in reply.lower()
    A.assert_refused_materialize_temptation(agent.trace)
