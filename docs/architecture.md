# Architecture

```
upstream repos ──verify──▶ candidates ──import (gates)──▶ ~/skill-library   (cold: no agent scans it)
                                                            │  skills/ · packs/ · git history
                             skill-add @web-vue @postgres ──┘
                                                            ▼
                    your-project/.agents/skills/<skill>/              real copy, committed (Codex, OpenCode)
                    your-project/.claude/skills/<skill> → ../../.agents/skills/<skill>    (Claude Code)
                    your-project/.agents/skill-manifest.json          packs, source commit, content hash, pins
```

## Repository layout

```
skillman/                          this repository (MIT, tooling only)
├── bin/
│   ├── skillman                   Textual TUI: PEP 723 script, dependencies locked in skillman.lock
│   ├── skill-add                  installer wrapper around skillcore.py install
│   └── skill-search               awk search over INDEX.md
├── scripts/
│   ├── skillcore.py               all logic (stdlib + PyYAML): packs, projects, trash, updates, audit
│   ├── verify-repos.py            sparse clone of upstream repos → staging/CANDIDATES.md
│   ├── tick-candidates.py         tick candidates by pack, tier or repo
│   ├── import-skills.py           import ticked candidates through the safety pipeline
│   ├── validate-library.py        validate the library, rebuild the index
│   ├── build-packs.py             regenerate vendor packs, create missing stack packs
│   └── systemd/                   optional weekly update timer
├── skills/skillman/               a skill that teaches agents to use these tools
├── packs/                         34 starter packs
├── staging/repos.txt              curated upstream list: tier, group, owner/repo, subpath hint
├── tests/                         offline tests: fake git upstream, Textual pilot
├── tools/make_screenshots.py      renders docs/screenshots from a copy of a real library
└── docs/                          this documentation
```

## Library layout

```
~/skill-library/                   your data, its own git repository (never published)
├── skills/<name>/                 SKILL.md, SOURCE.json, UPSTREAM-LICENSE.txt, resources
├── packs/*.yaml, packs/vendor/    your packs
├── staging/                       repos.txt, candidates, verify log
├── .cache/                        gitignored: blob-less clones, staged and pending updates
├── INDEX.md|json, VALIDATION.md   generated
├── IMPORT_LOG.md, UPDATE_LOG.md   generated
└── scripts → skillman/scripts     installed by install.sh
```

## Project manifest

`<project>/.agents/skill-manifest.json` records what skillman put in the project:

```json
{
  "version": 1,
  "skills": {
    "vitest": {
      "source": "vitest-dev/vitest",
      "upstream_commit": "b41e…",
      "tree_hash": "08caea22b554f674",
      "installed_at": "2026-11-21T15:10:08+0700",
      "packs": ["web-vue"],
      "manual": false,
      "pinned": false
    }
  },
  "packs": { "web-vue": { "installed_at": "2026-11-21T15:10:08+0700" } }
}
```

`tree_hash` is a SHA-256 over every file's relative path and bytes (symlinks by target), ignoring `__pycache__` and
`.DS_Store`. Comparing three hashes gives the skill's state: the recorded one, the project copy's and the library's.
A copy that matches neither the record nor the library is a `conflict`.

Machine state lives outside projects: the project registry is `~/.local/state/skillman/projects.json`, and removed
copies go to `~/.local/state/skillman/trash/`. If that directory is not writable, skillman uses
`<project>/.agents/.skill-trash`, which is gitignored.

## Design decisions

- **Copies, not links into the library.** A project must behave the same on a teammate's machine and in CI, so its
  skills are committed with it. The manifest hash is what keeps copies manageable.
- **Cold by default.** Global skill folders stay small. Per-project skills load only where they are relevant, which
  keeps the description budget, and the agent's attention, for the skills that matter.
- **One core, thin shells.** `skillcore.py` is importable and scriptable. The TUI, `skill-add`, the timer and the
  tests all drive the same functions.
- **Git for history, trash for the rest.** Library changes are commits you can revert. Project removals are moves
  you can restore. Nothing is deleted without an explicit "empty trash".
- **Refuse rather than guess.** Symlinked `.agents/` or `.claude/skills/` folders, real folders where a link
  belongs, invalid names, path traversal in names and a dirty library working tree are refused with a message.
- **Locks and atomic writes.** Manifest and pack writes take an `fcntl` lock and go through a temp file plus rename,
  so the TUI and a CLI run at the same time cannot corrupt each other's state.
