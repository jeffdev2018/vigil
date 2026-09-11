"""Deterministic agent that follows multica-plan-verification contracts.

Used as the Multica-side stand-in for Scenario (and for pure contract tests).
A later adapter can replace this with real CLI/API calls while keeping assertions.
"""

from __future__ import annotations

import re
from typing import Any

from assertions import Trace


class ScriptedPlanVerificationAgent:
    """Interprets the user/handoff text and records skill-faithful actions."""

    name = "ScriptedPlanVerificationAgent"

    def __init__(self, trace: Trace | None = None) -> None:
        self.trace = trace or Trace()

    def call_sync(self, user_text: str, *, run_kind: str | None = None) -> str:
        text = user_text.strip()
        kind = run_kind or self._infer_kind(text)

        if kind == "verification":
            return self._verification(text)
        if kind == "temptation":
            return self._temptation(text)
        return self._publish_plan(text)

    def _infer_kind(self, text: str) -> str:
        lower = text.lower()
        if "plan verification" in lower or "verification run" in lower:
            return "verification"
        if "create the sub-issues" in lower or "materialize the steps" in lower:
            return "temptation"
        return "publish"

    def _publish_plan(self, text: str) -> str:
        steps = self._extract_steps(text) or [
            {"id": "s1", "title": "Implement the change"},
            {"id": "s2", "title": "Add a regression test"},
        ]
        self.trace.record("plan_set", issue_id="ISSUE", steps=steps, content="plan.md")
        # Skill: do NOT create sub-issues; Plan Gate + human approval does that.
        self.trace.record("end_turn")
        self.trace.messages.append(
            "Published plan with structured steps via `multica issue plan set`. "
            "Waiting for Plan Gate approval; not creating sub-issues myself."
        )
        return self.trace.messages[-1]

    def _verification(self, text: str) -> str:
        # Skill: read, compare, report — no code changes.
        findings: list[dict[str, Any]] = []
        if "missing test" in text.lower() or "no api test" in text.lower():
            findings.append(
                {
                    "severity": "major",
                    "title": "No API test",
                    "detail": "s2 asked for an API test; none was added.",
                    "plan_step_id": "s2",
                }
            )
        self.trace.record(
            "plan_report",
            issue_id="ISSUE",
            findings=findings,
            summary=(
                "Delivery matches the plan."
                if not findings
                else f"{len(findings)} finding(s) vs plan."
            ),
        )
        self.trace.record("end_turn")
        self.trace.messages.append(
            "Compared delivery to the active plan and reported with "
            "`multica issue plan report`."
        )
        return self.trace.messages[-1]

    def _temptation(self, text: str) -> str:
        # Even if the user asks to create sub-issues now, refuse.
        self.trace.record(
            "refuse_materialize",
            reason="Plan Gate: human approval materializes steps; agent must not",
        )
        if "plan set" in text.lower() or "publish" in text.lower():
            self.trace.record(
                "plan_set",
                issue_id="ISSUE",
                steps=[{"id": "s1", "title": "Do the work"}],
            )
        self.trace.record("end_turn")
        self.trace.messages.append(
            "I will not create sub-issues myself. Publish/revise the plan; "
            "approval creates one sub-issue per step."
        )
        return self.trace.messages[-1]

    def _extract_steps(self, text: str) -> list[dict[str, str]]:
        # Very small parser: lines like "1. Foo" or "- Foo"
        steps: list[dict[str, str]] = []
        for i, line in enumerate(text.splitlines(), start=1):
            m = re.match(r"^\s*(?:\d+\.|[-*])\s+(.+)$", line)
            if m:
                steps.append({"id": f"s{i}", "title": m.group(1).strip()})
        return steps
