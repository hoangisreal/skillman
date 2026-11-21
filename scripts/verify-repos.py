#!/usr/bin/env python3
"""Verify upstream skill repos (read-only) and list candidate skills for an allowlist.

Usage: verify-repos.py [repos.txt] [-j 8]
  repos.txt lines: <tier> <group> <owner/repo> [subpath-hint]   ('#' = comment)
For each repo: shallow, blob-less, sparse clone into .cache/repos/<owner>__<repo> (persistent; reused by
import-skills.py and skillcore update-library),
fetch only SKILL.md (+ root LICENSE*) blobs, parse frontmatter. Nothing is imported.
Writes staging/CANDIDATES.md, staging/candidates.json, staging/VERIFY_LOG.md.
"""
from __future__ import annotations
import json, os, re, subprocess, sys, time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import yaml

sys.path.insert(0, str(Path(__file__).resolve().parent))
import skillcore  # noqa: E402

LIB, STAGING, WORK = skillcore.LIB, skillcore.STAGING, skillcore.REPO_CACHE
EXISTING = {p.name for p in (LIB / "skills").iterdir() if p.is_dir()}
# never treat these as skills, even if a SKILL.md sits inside them
SKIP_PARTS = {"node_modules", "test", "tests", "fixtures", "examples", "templates", ".github"}


def git(args, cwd=None, timeout=180):
    return subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True, timeout=timeout,
                          env={**os.environ, "GIT_TERMINAL_PROMPT": "0"})


def frontmatter(path: Path):
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return {}, 0
    lines = text.count("\n") + 1
    if not text.startswith("---"):
        return {}, lines
    try:
        fm = yaml.safe_load(text.split("---", 2)[1]) or {}
        return (fm if isinstance(fm, dict) else {}), lines
    except Exception:
        return {"_yaml_error": True}, lines


def verify(entry):
    tier, group, repo, hint = entry
    dest = WORK / repo.replace("/", "__")
    url = skillcore.remote_url(repo)
    t0 = time.time()
    r = git(["ls-remote", url, "HEAD"], timeout=60)
    if r.returncode != 0 or not r.stdout.strip():
        return {"repo": repo, "tier": tier, "group": group, "status": "unreachable",
                "error": (r.stderr.strip().splitlines() or ["no HEAD"])[-1][:200], "skills": []}
    commit = r.stdout.split()[0]
    if dest.exists():
        subprocess.run(["rm", "-rf", "--", str(dest)], check=False)
    r = git(["clone", "--depth", "1", "--filter=blob:none", "--sparse", "--no-checkout", "-q", url, str(dest)])
    if r.returncode != 0:
        return {"repo": repo, "tier": tier, "group": group, "status": "clone-failed", "commit": commit,
                "error": r.stderr.strip()[-200:], "skills": []}
    tree = git(["ls-tree", "-r", "--name-only", "HEAD"], cwd=dest).stdout.splitlines()
    paths = [p for p in tree if p.endswith("/SKILL.md") or p == "SKILL.md"]
    paths = [p for p in paths if not (set(Path(p).parts[:-1]) & SKIP_PARTS)]
    licenses = [p for p in tree if "/" not in p and re.match(r"(?i)^(license|licence|copying)", p)]
    status = "ok"
    if hint:
        hinted = [p for p in paths if p.startswith(hint.rstrip("/") + "/") or p == f"{hint.rstrip('/')}/SKILL.md"]
        if hinted:
            paths = hinted
        else:
            status = "subpath-changed"
    if not paths:
        return {"repo": repo, "tier": tier, "group": group, "status": "no-skills", "commit": commit,
                "licenses": licenses, "skills": []}
    git(["sparse-checkout", "set", "--no-cone", *paths, *licenses], cwd=dest)
    git(["checkout", "-q", "HEAD"], cwd=dest, timeout=300)
    skills = []
    for p in paths:
        fm, lines = frontmatter(dest / p)
        skill_dir = str(Path(p).parent) if "/" in p else "."
        name = str(fm.get("name") or Path(skill_dir).name or repo.split("/")[1])
        desc = " ".join(str(fm.get("description") or "").split())
        skills.append({
            "name": name, "path": skill_dir, "lines": lines,
            "description": desc[:220],
            "collides": name in EXISTING,
            "yaml_error": bool(fm.get("_yaml_error")),
            "has_hooks": "hooks" in fm,
        })
    return {"repo": repo, "tier": tier, "group": group, "status": status, "commit": commit,
            "licenses": licenses, "skills": skills, "seconds": round(time.time() - t0, 1)}


def main():
    args = [a for a in sys.argv[1:]]
    jobs = 8
    if "-j" in args:
        i = args.index("-j"); jobs = int(args[i + 1]); del args[i:i + 2]
    src = Path(args[0]) if args else STAGING / "repos.txt"
    entries = []
    for line in src.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        f = line.split()
        entries.append((f[0], f[1], f[2], f[3] if len(f) > 3 else ""))
    WORK.mkdir(parents=True, exist_ok=True)
    results = []
    with ThreadPoolExecutor(max_workers=jobs) as ex:
        for res in ex.map(verify, entries):
            n = len(res["skills"])
            print(f"{res['status']:16} {res['repo']:55} {n:4} skills", flush=True)
            results.append(res)

    (STAGING / "candidates.json").write_text(json.dumps(results, indent=1, ensure_ascii=False) + "\n")
    ok = [r for r in results if r["skills"]]
    total = sum(len(r["skills"]) for r in ok)
    md = ["# Upstream skill candidates", "",
          f"Verified {len(results)} repos · {len(ok)} with skills · {total} candidate skills · "
          f"generated {time.strftime('%Y-%m-%d %H:%M')}", "",
          "Mark the ones to import by changing `[ ]` to `[x]`. `collides` = a skill with that name already exists in the library.", ""]
    for group in ("core", "data", "ops"):
        md.append(f"## {group}")
        for r in [r for r in ok if r["group"] == group]:
            lic = ",".join(r.get("licenses") or []) or "NO ROOT LICENSE"
            md.append(f"\n### {r['repo']} ({r['tier']}) · `{r['commit'][:12]}` · {lic}" +
                      (" · ⚠ subpath changed" if r["status"] == "subpath-changed" else ""))
            for s in sorted(r["skills"], key=lambda s: s["name"]):
                flags = " ".join(f for f, on in [("collides", s["collides"]), ("hooks", s["has_hooks"]),
                                                  ("yaml-error", s["yaml_error"]), (">500 lines", s["lines"] > 500)] if on)
                md.append(f"- [ ] `{s['name']}` — {s['description'][:140]} "
                          f"<sub>{s['path']} · {s['lines']}L{(' · ' + flags) if flags else ''}</sub>")
        md.append("")
    (STAGING / "CANDIDATES.md").write_text("\n".join(md) + "\n")

    log = ["# Verify log", "", "| repo | status | detail |", "|---|---|---|"]
    for r in results:
        if r["status"] != "ok":
            log.append(f"| {r['repo']} | {r['status']} | {r.get('error', '') or len(r['skills'])} |")
    (STAGING / "VERIFY_LOG.md").write_text("\n".join(log) + "\n")
    print(f"\n{len(ok)}/{len(results)} repos with skills, {total} candidates -> {STAGING/'CANDIDATES.md'}")


if __name__ == "__main__":
    main()
