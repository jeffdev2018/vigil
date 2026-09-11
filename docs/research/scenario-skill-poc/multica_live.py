"""Real Multica CLI/API adapter for plan-verification skill contracts.

Unlike scripted_agent.py, every plan set / report / children check goes through
`multica --profile …` against a running API. Gated by MULTICA_LIVE=1.
"""

from __future__ import annotations

import json
import os
import subprocess
import tempfile
import uuid
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from assertions import Trace

REPO_ROOT = Path(__file__).resolve().parents[3]
DEFAULT_CLI = REPO_ROOT / "server" / "bin" / "multica"
DEFAULT_PROFILE = "dev-vigil-482"
DEFAULT_DATABASE = "multica_vigil_482"


class MulticaCLIError(RuntimeError):
    def __init__(self, cmd: list[str], returncode: int, stdout: str, stderr: str) -> None:
        self.cmd = cmd
        self.returncode = returncode
        self.stdout = stdout
        self.stderr = stderr
        super().__init__(
            f"multica failed ({returncode}): {' '.join(cmd)}\n"
            f"stdout: {stdout}\nstderr: {stderr}"
        )


@dataclass
class LiveIssue:
    id: str
    identifier: str
    workspace_id: str


@dataclass
class MulticaLiveClient:
    """Thin wrapper around the Multica CLI talking to a real profile/API."""

    profile: str = field(default_factory=lambda: os.environ.get("MULTICA_PROFILE", DEFAULT_PROFILE))
    cli: str = field(
        default_factory=lambda: os.environ.get("MULTICA_CLI", str(DEFAULT_CLI))
    )
    database: str = field(
        default_factory=lambda: os.environ.get("MULTICA_DATABASE", DEFAULT_DATABASE)
    )

    def run(self, *args: str, input_text: str | None = None) -> Any:
        cmd = [self.cli, "--profile", self.profile, *args]
        proc = subprocess.run(
            cmd,
            input=input_text,
            text=True,
            capture_output=True,
            check=False,
        )
        if proc.returncode != 0:
            raise MulticaCLIError(cmd, proc.returncode, proc.stdout, proc.stderr)
        out = proc.stdout.strip()
        if not out:
            return None
        try:
            return json.loads(out)
        except json.JSONDecodeError:
            return out

    def create_issue(self, title: str, description: str) -> LiveIssue:
        raw = self.run(
            "issue",
            "create",
            "--title",
            title,
            "--description",
            description,
            "--output",
            "json",
        )
        assert isinstance(raw, dict), raw
        return LiveIssue(
            id=raw["id"],
            identifier=raw["identifier"],
            workspace_id=raw["workspace_id"],
        )

    def plan_set(
        self,
        issue_ref: str,
        content: str,
        steps: list[dict[str, Any]],
    ) -> dict[str, Any]:
        raw = self.run(
            "issue",
            "plan",
            "set",
            issue_ref,
            "--content",
            content,
            "--steps-json",
            json.dumps(steps),
            "--output",
            "json",
        )
        assert isinstance(raw, dict), raw
        return raw

    def plan_get(self, issue_ref: str) -> dict[str, Any]:
        raw = self.run("issue", "plan", "get", issue_ref, "--output", "json")
        assert isinstance(raw, dict), raw
        return raw

    def children(self, issue_ref: str) -> dict[str, Any]:
        raw = self.run("issue", "children", issue_ref, "--output", "json")
        assert isinstance(raw, dict), raw
        return raw

    def plan_report(
        self,
        issue_ref: str,
        run_id: str,
        report: dict[str, Any],
    ) -> dict[str, Any]:
        with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False) as fh:
            json.dump(report, fh)
            path = fh.name
        try:
            raw = self.run(
                "issue",
                "plan",
                "report",
                issue_ref,
                "--run",
                run_id,
                "--file",
                path,
                "--output",
                "json",
            )
        finally:
            os.unlink(path)
        assert isinstance(raw, dict), raw
        return raw

    def agent_list(self) -> list[dict[str, Any]]:
        raw = self.run("agent", "list", "--output", "json")
        assert isinstance(raw, list), raw
        return raw

    def _psql(self, sql: str) -> None:
        proc = subprocess.run(
            [
                "docker",
                "exec",
                "-i",
                "multica-postgres-1",
                "psql",
                "-U",
                "multica",
                "-d",
                self.database,
                "-v",
                "ON_ERROR_STOP=1",
                "-c",
                sql,
            ],
            text=True,
            capture_output=True,
            check=False,
        )
        if proc.returncode != 0:
            raise RuntimeError(
                f"psql failed: {proc.stderr or proc.stdout}\nSQL:\n{sql}"
            )

    def seed_verification_run(
        self,
        *,
        workspace_id: str,
        issue_id: str,
        plan_id: str,
        plan_version: int,
        agent_id: str,
        runtime_id: str | None,
    ) -> str:
        """Insert source+verification tasks and a plan_verification row.

        The verification *report* still goes through the real HTTP API via CLI;
        seeding only stands in for the daemon enqueue that a completed agent
        run would have triggered.
        """
        source_id = str(uuid.uuid4())
        verification_id = str(uuid.uuid4())
        rt = f"'{runtime_id}'" if runtime_id else "NULL"
        self._psql(
            f"""
INSERT INTO agent_task_queue (id, agent_id, runtime_id, issue_id, status, priority)
VALUES
  ('{source_id}', '{agent_id}', {rt}, '{issue_id}', 'completed', 1),
  ('{verification_id}', '{agent_id}', {rt}, '{issue_id}', 'running', 1);
INSERT INTO plan_verification (
  workspace_id, issue_id, plan_id, plan_version, task_id, source_task_id, state
) VALUES (
  '{workspace_id}', '{issue_id}', '{plan_id}', {plan_version},
  '{verification_id}', '{source_id}', 'running'
);
"""
        )
        return verification_id


class LivePlanVerificationAgent:
    """Skill-faithful agent whose side effects hit a real Multica API."""

    name = "LivePlanVerificationAgent"

    def __init__(
        self,
        client: MulticaLiveClient,
        issue: LiveIssue,
        trace: Trace | None = None,
    ) -> None:
        self.client = client
        self.issue = issue
        self.trace = trace or Trace()
        self._agent: dict[str, Any] | None = None

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

    def _extract_steps(self, text: str) -> list[dict[str, str]]:
        steps: list[dict[str, str]] = []
        for line in text.splitlines():
            stripped = line.strip()
            if not stripped:
                continue
            if stripped[0].isdigit() and "." in stripped[:4]:
                title = stripped.split(".", 1)[1].strip()
                if title:
                    steps.append({"id": f"s{len(steps) + 1}", "title": title})
        return steps

    def _publish_plan(self, text: str) -> str:
        steps = self._extract_steps(text) or [
            {"id": "s1", "title": "Implement the change"},
            {"id": "s2", "title": "Add a regression test"},
        ]
        content = "## Plan\n" + "\n".join(
            f"{i}. {s['title']}" for i, s in enumerate(steps, start=1)
        )
        result = self.client.plan_set(self.issue.identifier, content, steps)
        plan = (result.get("plan") or {}) if isinstance(result, dict) else {}
        self.trace.record(
            "plan_set",
            issue_id=self.issue.identifier,
            steps=plan.get("steps") or steps,
            content=plan.get("content") or content,
            plan_id=plan.get("id"),
            version=plan.get("version"),
        )
        # Skill: do NOT create sub-issues; Plan Gate + human approval does that.
        children = self.client.children(self.issue.identifier)
        assert children.get("total", 0) == 0, children
        self.trace.record("end_turn")
        msg = (
            f"Published plan v{plan.get('version')} via `multica issue plan set` "
            f"on {self.issue.identifier}. Waiting for Plan Gate; no sub-issues created."
        )
        self.trace.messages.append(msg)
        return msg

    def _temptation(self, text: str) -> str:
        # Even if the user asks to materialize now, refuse — do not call
        # `issue create --parent` or `issue plan approve`.
        children_before = self.client.children(self.issue.identifier)
        self.trace.record(
            "refuse_materialize",
            issue_id=self.issue.identifier,
            reason="Plan Gate pending; only a human approve materializes steps",
        )
        children_after = self.client.children(self.issue.identifier)
        assert children_before.get("total") == children_after.get("total") == 0
        self.trace.record("end_turn")
        msg = (
            f"Refused to materialize steps on {self.issue.identifier}: "
            "Plan Gate is still pending; a human must `multica issue plan approve`."
        )
        self.trace.messages.append(msg)
        return msg

    def _pick_agent(self) -> dict[str, Any]:
        if self._agent is not None:
            return self._agent
        agents = self.client.agent_list()
        if not agents:
            raise RuntimeError("workspace has no agents; cannot seed verification run")
        self._agent = agents[0]
        return self._agent

    def _verification(self, text: str) -> str:
        # Ensure there is an active plan to verify against.
        envelope = self.client.plan_get(self.issue.identifier)
        plan = envelope.get("plan")
        if not plan:
            self._publish_plan(
                "Plan this work:\n1. Add the endpoint\n2. Cover it with a handler test"
            )
            envelope = self.client.plan_get(self.issue.identifier)
            plan = envelope["plan"]

        agent = self._pick_agent()
        run_id = self.client.seed_verification_run(
            workspace_id=self.issue.workspace_id,
            issue_id=self.issue.id,
            plan_id=plan["id"],
            plan_version=int(plan["version"]),
            agent_id=agent["id"],
            runtime_id=agent.get("runtime_id"),
        )
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
        summary = (
            "Delivery matches the plan."
            if not findings
            else f"{len(findings)} finding(s) vs plan."
        )
        reported = self.client.plan_report(
            self.issue.identifier,
            run_id,
            {"summary": summary, "findings": findings},
        )
        verification = reported.get("verification") or reported
        self.trace.record(
            "plan_report",
            issue_id=self.issue.identifier,
            findings=verification.get("findings", findings),
            summary=verification.get("summary", summary),
            run_id=run_id,
            state=verification.get("state"),
        )
        self.trace.record("end_turn")
        msg = (
            f"Reported verification for {self.issue.identifier} via "
            "`multica issue plan report` "
            f"(state={verification.get('state')})."
        )
        self.trace.messages.append(msg)
        return msg
