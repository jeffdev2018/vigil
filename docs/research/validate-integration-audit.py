"""Check the frozen integration audit's structure, not runtime capabilities."""

import json
import re
from datetime import datetime
from pathlib import Path
from urllib.parse import unquote, urlsplit


def main():
    root = Path(__file__).resolve().parent
    report = root / "integration-candidates-2026-09-05.md"
    text = report.read_text(encoding="utf-8")
    data = json.loads(report.with_suffix(".json").read_text(encoding="utf-8"))
    required = {
        "trycompai/crm", "twentyhq/twenty", "margince/margince",
        "buildkite/agent", "agentscope-ai/AgentTeams",
        "Untrivial-ai/agent-orchestrator", "Infisical/agent-vault",
        "TencentCloud/CubeSandbox",
    }
    rows = data["candidates"]
    assert len(rows) == len(required)
    assert {row["repo"] for row in rows} == required
    assert not any(marker in text for marker in ("Avelis", "TODO", "TBD"))
    source_urls = set()
    for row in rows:
        assert row["repo"] in text
        assert row["remote"] == "https://github.com/" + row["repo"]
        assert re.fullmatch(r"[0-9a-f]{40}", row["sha"])
        datetime.fromisoformat(row["commit_date"].replace("Z", "+00:00"))
        assert row["license_observed"] and row["branch"] and row["language"]
        assert isinstance(row["archived"], bool)
        assert row["verdict"] in {"FORK_SHORTLIST", "ADAPTER_ONLY", "EXCLUDE"}
        assert re.search(
            r"\| " + re.escape(row["repo"]) + r" \|[^\n]*`"
            + row["verdict"] + r"`", text
        ), row["repo"]
        assert row["runtime_probe"] == "not_run"
        assert row["sources"]
        assert any("LICENSE" in source["path"] for source in row["sources"])
        for source in row["sources"]:
            assert source["url"] == (
                row["remote"] + "/blob/" + row["sha"] + "/" + source["path"]
            )
            assert re.fullmatch(r"[0-9a-f]{64}", source["sha256"])
            source_urls.add(source["url"])
    for target in re.findall(r"\]\(([^)]+)\)", text):
        url = urlsplit(target)
        if url.scheme:
            assert url.scheme == "https" and url.netloc == "github.com", target
            if "/blob/" in url.path:
                assert target.split("#")[0] in source_urls, target
            else:
                assert any(target == r["remote"] + "/tree/" + r["sha"] for r in rows)
        else:
            assert (root / unquote(url.path)).is_file(), target
    print(f"OK: {len(rows)} candidates, {len(source_urls)} frozen sources, local links valid")


if __name__ == "__main__":
    main()
