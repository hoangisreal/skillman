#!/usr/bin/env python3
"""Import skills ticked `[x]` in staging/CANDIDATES.md into skills/.

Usage: import-skills.py [--dry-run] [--update] [--rename-collisions]
  Uses the persistent clone cache (.cache/repos, see skillcore.ensure_clone), pinned to the
  commit recorded in staging/candidates.json. Copies the WHOLE skill directory except plugin
  wiring (hooks/, commands/, agents/*.md, .mcp.json, plugin.json, .claude-plugin/) and symlinks.
  Requires a license (skill-local or repo root -> UPSTREAM-LICENSE.txt).
  Existing skills are never overwritten unless --update, and never when SOURCE.json lists
  local_modifications. Name collisions with another source are skipped, or imported as
  <owner>-<name> with --rename-collisions. Appends results to IMPORT_LOG.md.
"""
from __future__ import annotations
import json, sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import skillcore as sc  # noqa: E402

DRY, UPDATE, RENAME = "--dry-run" in sys.argv, "--update" in sys.argv, "--rename-collisions" in sys.argv


def main():
    meta = {r["repo"]: r for r in json.loads((sc.STAGING / "candidates.json").read_text())}
    picks = [c for c in sc.read_candidates() if c["ticked"]]
    if not picks:
        print("nothing ticked in staging/CANDIDATES.md"); return 0
    log, stamp = [], sc.now()
    sc.CACHE.mkdir(parents=True, exist_ok=True)
    for c in picks:
        repo, name, path = c["repo"], c["name"], c["path"]
        r = meta.get(repo)
        if not r or not r.get("commit"):
            log.append((repo, name, "FAILED", "repo not in candidates.json; rerun verify-repos.py")); continue
        dest_name, target = name, sc.SKILLS / name
        if target.exists():
            src = sc.load_json(target / "SOURCE.json")
            same_origin = src.get("repo") == repo
            if same_origin and UPDATE and not src.get("local_modifications"):
                pass
            elif same_origin:
                log.append((repo, name, "SKIPPED", "exists (use --update)" if not src.get("local_modifications")
                            else "has local_modifications (use skillcore.py update-library)")); continue
            elif RENAME:
                dest_name = f"{repo.split('/')[0].lower()}-{name}"
                target = sc.SKILLS / dest_name
                if target.exists():
                    log.append((repo, name, "SKIPPED", f"{dest_name} also exists")); continue
            else:
                log.append((repo, name, "SKIPPED", f"name collides with {src.get('repo') or 'local skill'}")); continue
        if not sc.KEBAB.match(dest_name):
            log.append((repo, name, "SKIPPED", "name not kebab-case")); continue
        try:
            staged, mods = sc.materialize(repo, r["commit"], path, dest_name, r.get("tier", ""))
        except sc.SkillError as e:
            kind = "SKIPPED" if "license" in str(e) else "FAILED"
            log.append((repo, name, kind, str(e))); continue
        if DRY:
            sc.shutil.rmtree(staged, ignore_errors=True)
            log.append((repo, name, "DRY-RUN", f"-> skills/{dest_name}" + (f" ({'; '.join(mods)})" if mods else ""))); continue
        existed = sc.install_into_library(staged, dest_name)
        log.append((repo, name, "UPDATED" if existed else "IMPORTED", f"skills/{dest_name}"))

    lines = [f"\n## {stamp}{' (dry-run)' if DRY else ''}", "", "| repo | skill | result | detail |", "|---|---|---|---|"]
    lines += [f"| {a} | {b} | {c} | {d.replace('|', '/')} |" for a, b, c, d in log]
    with open(sc.LIB / "IMPORT_LOG.md", "a") as f:
        if f.tell() == 0:
            f.write("# Import log\n")
        f.write("\n".join(lines) + "\n")
    for a, b, c, d in log:
        print(f"{c:9} {a}:{b}  {d}")
    if not DRY and any(c in ("IMPORTED", "UPDATED") for _, _, c, _ in log):
        print("next: python3 scripts/validate-library.py  (rebuild INDEX), then review + git commit")
    return 0


if __name__ == "__main__":
    sys.exit(main())
