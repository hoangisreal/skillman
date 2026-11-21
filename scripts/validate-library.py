#!/usr/bin/env python3
"""Validate every skills/*/SKILL.md and (re)build INDEX.md + INDEX.json.

Usage: validate-library.py [--no-index] [--strict] [-v]
  --strict   exit 1 if any ERROR is found (warnings never fail).
Writes VALIDATION.md (full findings) next to INDEX.md. Logic lives in skillcore.py.
"""
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import skillcore  # noqa: E402


def main() -> int:
    rows, errors, warns = skillcore.rebuild_index(write_index="--no-index" not in sys.argv)
    print(f"skills={len(rows)} errors={len(errors)} warnings={len(warns)}  (details: {skillcore.LIB / 'VALIDATION.md'})")
    probs = skillcore.validate_packs()
    missing = [(n, m) for n, m in probs if m.startswith("skill not in library")]
    other = [(n, m) for n, m in probs if (n, m) not in missing]
    for n, m in other:
        print(f"pack problem: {n}: {m}")
    if missing:
        packs = sorted({n for n, _ in missing})
        print(f"packs reference {len(missing)} skill(s) not in the library yet ({len(packs)} packs: {', '.join(packs[:6])}"
              f"{' …' if len(packs) > 6 else ''}). Import them: scripts/tick-candidates.py --pack <name> && scripts/import-skills.py"
              + ("" if "-v" not in sys.argv else "\n" + "\n".join(f"  {n}: {m}" for n, m in missing)))
    return 1 if ("--strict" in sys.argv and (errors or probs)) else 0


if __name__ == "__main__":
    sys.exit(main())
