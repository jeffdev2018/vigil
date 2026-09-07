"""Validate the bounded Kokpit report and its locally frozen source evidence."""
import hashlib
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parent
report = (root / "kokpit-candidates-2026-09-05.md").read_text(encoding="utf-8")
manifest = json.loads((root / "kokpit-candidates-2026-09-05.json").read_text(encoding="utf-8"))
headings = ["Périmètre et verdict", "Matrice de décision", "Fonctionnalités à retenir",
            "Avantage commercial à éprouver", "Séquence et test commercial", "Sources et vérification"]
assert all("## " + title in report for title in headings)
matrix = report.split("## Matrice de décision\n", 1)[1].split("\n## ", 1)[0]
rows = [line for line in matrix.splitlines() if line.startswith("| ")][1:]
assert len(rows) == len(manifest["candidates"]) == 8
assert all(len(row.split("|")) == 11 for row in rows)
assert "Build gate" in report and "Kill condition" in report
assert not re.search(r"\b(?:TODO|TBD|PLACEHOLDER)\b", report)
assert manifest["inventoried_html"] == 1311
count = 0
for candidate in manifest["candidates"]:
    assert re.fullmatch(r"[0-9a-f]{40}", candidate["commit"])
    source_root = Path(candidate["local_path"])
    assert len({item["path"] for item in candidate["files"]}) == len(candidate["files"])
    for item in candidate["files"]:
        source = source_root / item["path"]
        assert source.resolve().is_relative_to(source_root.resolve())
        assert hashlib.sha256(source.read_bytes()).hexdigest() == item["sha256"], source
        count += 1
assert count == 60
print(f"OK: 8 decisions, {count} source hashes, build/kill gates, UTF-8 report")
