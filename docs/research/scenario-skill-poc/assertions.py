"""Recorded actions + assertions for multica-plan-verification skill contracts.

Mirrors the skill text in
server/internal/service/builtin_skills/multica-plan-verification/SKILL.md
without calling Multica or an LLM.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class Action:
    kind: str
    payload: dict[str, Any] = field(default_factory=dict)


@dataclass
class Trace:
    actions: list[Action] = field(default_factory=list)
    messages: list[str] = field(default_factory=list)

    def record(self, kind: str, **payload: Any) -> None:
        self.actions.append(Action(kind=kind, payload=payload))

    def kinds(self) -> list[str]:
        return [a.kind for a in self.actions]


def assert_published_plan_with_steps(trace: Trace) -> None:
    sets = [a for a in trace.actions if a.kind == "plan_set"]
    assert sets, "expected multica issue plan set"
    last = sets[-1]
    steps = last.payload.get("steps") or []
    assert len(steps) >= 1, "plan set must carry structured steps"
    assert "plan_set" in trace.kinds()


def assert_did_not_create_sub_issues(trace: Trace) -> None:
    created = [a for a in trace.actions if a.kind == "create_sub_issue"]
    assert not created, (
        "Plan Gate: agent must not create sub-issues after publishing steps; "
        f"got {len(created)} create_sub_issue action(s)"
    )


def assert_ended_turn_after_plan(trace: Trace) -> None:
    assert "end_turn" in trace.kinds(), "expected end_turn after publishing plan"
    # end_turn should be last meaningful action after plan_set
    kinds = trace.kinds()
    assert kinds.index("plan_set") < kinds.index("end_turn")


def assert_verification_reported(trace: Trace) -> None:
    reports = [a for a in trace.actions if a.kind == "plan_report"]
    assert reports, "verification run must call plan report"
    findings = reports[-1].payload.get("findings")
    assert isinstance(findings, list), "findings must be a JSON array (may be empty)"
    assert "code_change" not in trace.kinds(), "verification run must not change code"


def assert_refused_materialize_temptation(trace: Trace) -> None:
    assert_did_not_create_sub_issues(trace)
    refused = [a for a in trace.actions if a.kind == "refuse_materialize"]
    assert refused, "expected an explicit refuse_materialize when tempted"
