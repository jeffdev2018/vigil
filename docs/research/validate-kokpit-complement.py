"""Check the four additional decisions and their frozen evidence; no network."""
import hashlib
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parent
report = (root / "kokpit-complement-2026-09-05.md").read_text(encoding="utf-8")
manifest = json.loads((root / "kokpit-complement-2026-09-05.json").read_text(encoding="utf-8"))
for heading in ("Périmètre et verdict", "Matrice de décision", "Fonctionnalités à retenir",
                "Collisions avec le projet actuel", "Avantage commercial à éprouver",
                "Séquence et test commercial", "Sources et vérification"):
    assert "## " + heading in report, heading
matrix = report.split("## Matrice de décision\n", 1)[1].split("\n## ", 1)[0]
rows = [line for line in matrix.splitlines() if line.startswith("| ")][1:]
assert len(rows) == len(manifest) == 4
assert all(len(row.split("|")) == 11 for row in rows)
assert "Build gate" in report and "Kill condition" in report
assert not re.search(r"\b(?:TODO|TBD|PLACEHOLDER)\b", report)
count = 0
for candidate in manifest:
    assert re.fullmatch(r"[0-9a-f]{40}", candidate["commit"])
    source_root = Path(candidate["local_path"])
    assert str(source_root) in report or str(source_root.parent) + "/" in report
    assert len({item["path"] for item in candidate["files"]}) == len(candidate["files"])
    for item in candidate["files"]:
        source = source_root / item["path"]
        assert source.resolve().is_relative_to(source_root.resolve())
        assert candidate["commit"] in item["url"]
        assert hashlib.sha256(source.read_bytes()).hexdigest() == item["sha256"], source
        count += 1
assert count == 24
print("OK: 4 decisions, 24 source hashes, build/kill gates, UTF-8 report")
