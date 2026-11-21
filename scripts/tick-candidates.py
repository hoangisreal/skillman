#!/usr/bin/env python3
"""Tick candidates in staging/CANDIDATES.md by rule.

Usage: tick-candidates.py [--tier official] [--max-per-repo 40] [--repo owner/name ...] [--pack name ...] [--reset]
  --pack ticks exactly the candidates whose name is in the given packs (after `extends`), e.g. --pack core web-vue
Never ticks skills flagged `collides`. --reset unticks everything first.
"""
import argparse, json, sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import skillcore as sc  # noqa: E402

p = argparse.ArgumentParser()
p.add_argument("--tier"); p.add_argument("--max-per-repo", type=int, default=10**9)
p.add_argument("--repo", nargs="*", default=None); p.add_argument("--reset", action="store_true")
p.add_argument("--pack", nargs="*", default=None)
a = p.parse_args()

meta = {r["repo"]: r for r in json.loads((sc.STAGING / "candidates.json").read_text())}
ok = {name for name, r in meta.items()
      if (not a.tier or r["tier"] == a.tier) and len(r["skills"]) <= a.max_per_repo
      and (a.repo is None or name in a.repo)}
wanted = None
if a.pack is not None:
    wanted = {s for pk in a.pack for s in sc.expand_pack(pk.lstrip("@"))}
    ok = set() if a.repo is None and not a.tier else ok
cands = sc.read_candidates()
pick: dict[str, int] = {}  # --pack: one source per skill name; prefer repos with a root license, then repos.txt order
if wanted is not None:
    for c in sorted(cands, key=lambda c: not meta.get(c["repo"], {}).get("licenses")):
        if c["name"] in wanted and c["name"] not in pick and not c["collides"] and (c["repo"] in ok or not ok):
            pick[c["name"]] = c["line"]
changes = {}
for c in cands:
    on = c["ticked"] and not a.reset
    if wanted is not None and pick.get(c["name"]) == c["line"]:
        on = True
    elif wanted is None and c["repo"] in ok and not c["collides"]:
        on = True
    if on != c["ticked"]:
        changes[c["line"]] = on
sc.set_ticks(changes)
n = sum(c["ticked"] for c in sc.read_candidates())
if wanted is not None:
    found = {c["name"] for c in sc.read_candidates() if c["ticked"]} & wanted
    lib = set(sc.library_names())
    print(f"pack skills: {len(wanted)} · ticked: {len(found)} · already in library: {len(wanted & lib)}"
          + (f" · not in any verified repo: {', '.join(sorted(wanted - found - lib))}" if wanted - found - lib else ""))
print(f"ticked total: {n}")
