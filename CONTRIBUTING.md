# Contributing

Issues and pull requests are welcome. Please keep changes small and focused.

## Setup

```bash
git clone https://github.com/hoangisreal/skillman && cd skillman
SKILL_LIBRARY=$PWD/.dev-lib ./install.sh   # a scratch library, so your real one is untouched
```

Python 3.11 or newer with PyYAML and git are the only runtime requirements. The TUI and its tests also need
Textual 8.2.8, which `uv` provides without touching your system Python.

## Tests

```bash
uv run --no-project --with pyyaml python tests/test_skillcore.py
uv run --no-project --with textual==8.2.8 --with pyyaml python tests/test_tui.py
uvx ruff@0.8.4 check .
bash -n bin/skill-add bin/skill-search install.sh
```

The tests are fully offline. `tests/fixtures.py` points `SKILL_LIBRARY`, `SKILLMAN_STATE` and `SKILLCORE_GIT_BASE` at a
temp dir **before** importing `skillcore`, builds a fake upstream as a bare git repo served over `file://` with
partial-clone support, and imports from it. They cover:

- pack expansion and cycles;
- install, update, conflict, adopt and pin flows;
- trash round-trips;
- refusal of path traversal and symlinks;
- tree-OID change detection, safe and risky updates, license removal, symlink inlining and rollback;
- `new`, `verify` and `audit`;
- the TUI, driven key by key with Textual's pilot.

CI runs them on Python 3.11, 3.12 and 3.13.

## Guidelines

- Logic goes in `scripts/skillcore.py`. The TUI and `skill-add` only call it.
- Every write to user data needs a test. Anything that removes data must move it to the trash.
- New risk signals go in `SUSPICIOUS` or `assess()`, with a test that shows them being caught.
- Do not add runtime dependencies beyond PyYAML.
- Screenshots: `uv run --no-project --with textual==8.2.8 --with pyyaml python tools/make_screenshots.py [library]`
  renders them from a copy of a library, so the library itself is never modified.

## Adding upstream repositories

Add a line to `staging/repos.txt` (`tier group owner/repo [subpath-hint]`). Only repositories with a license are
imported. Large community collections are not bulk-imported: list them so that people can cherry-pick from them.
