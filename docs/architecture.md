# Architecture

```
upstream repos ──verify──▶ candidates ──import (gates)──▶ ~/skill-library   (cold: no agent scans it)
                                                            │  skills/ · packs/ · taxonomy.yaml · git history
                             skill-add @web-vue @postgres ──┘
                                                            ▼
                    your-project/.agents/skills/<skill>/              real copy, committed (Codex, OpenCode)
                    your-project/.claude/skills/<skill> → ../../.agents/skills/<skill>    (Claude Code)
                    your-project/.agents/skill-manifest.json          packs, source commit, content hash, pins
```

## Repository layout

```
skillman/                          this repository (MIT, tooling only)
├── cmd/skillman/                  main: one binary (TUI + CLI); invoked as skill-add / skill-search via links
├── internal/
│   ├── core/                      all logic: index, validation, packs, projects, trash, audit, verify,
│   │                              upstream updates, import pipeline, taxonomy (+ embedded taxonomy.yaml)
│   ├── search/                    in-memory search index and query language
│   ├── cli/                       every command, text and --json output
│   └── tui/                       Bubble Tea v2 UI: 8 tabs, virtualized tables, Markdown preview, modals
├── assets.go                      embeds packs/, staging/repos.txt and skills/skillman for `skillman init`
├── skills/skillman/               a skill that teaches agents to use skillman
├── packs/                         34 starter packs
├── staging/repos.txt              curated upstream list: tier, group, owner/repo, subpath hint
├── contrib/systemd/               optional weekly update timer
├── install.sh                     go build + links + init + agent-setup
└── docs/                          this documentation
```

## Library layout

```
~/skill-library/                   your data, its own git repository (never published)
├── skills/<name>/                 SKILL.md, SOURCE.json, UPSTREAM-LICENSE.txt, resources
├── packs/*.yaml, packs/vendor/    your packs
├── staging/                       repos.txt, candidates, verify log
├── .cache/                        gitignored: blob-less clones, staged/pending updates, classify.json
├── taxonomy.yaml                  optional: your domain/stack overrides (see taxonomy.md)
├── INDEX.md|json, VALIDATION.md   generated (INDEX.json rows carry domain and stacks)
└── IMPORT_LOG.md, UPDATE_LOG.md   generated
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

A skill created with `skillman new --project` (or tracked with `adopt --project`) has `"source": "project"` and no
upstream commit: it lives only in the project, `status` compares it with its recorded hash only, and `update <name>`
records an edit.

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
- **One core, thin shells.** `internal/core` holds every operation; the CLI, the TUI, `skill-add`, the timer and
  the tests all call the same functions, so anything the TUI can do an agent can do from the command line.
- **Compatible on disk.** The Go port reproduces the Python version's formats byte for byte where it matters
  (content hashes, `INDEX.json`, pack YAML, manifests, `VALIDATION.md`); golden tests generated from the Python
  version keep it that way.
- **Git for history, trash for the rest.** Library changes are commits you can revert. Project removals are moves
  you can restore. Nothing is deleted without an explicit "empty trash".
- **Refuse rather than guess.** Symlinked `.agents/` or `.claude/skills/` folders, real folders where a link
  belongs, symlinks inside a skill (Git for Windows checks them out as plain files), invalid names, path traversal
  in names and a dirty library working tree are refused with a message.
- **Locks and atomic writes.** Manifest and pack writes take an `flock` lock and go through a temp file plus rename,
  so the TUI and a CLI run at the same time cannot corrupt each other's state.

## Performance design

The 0.1 TUI rebuilt the whole table and re-rendered the Markdown preview on every keystroke and cursor move
(80 ms per key, 0.4–1.5 s per cursor move on 718 skills). 0.2 keeps every frame cheap:

- **Prebuilt search index**: lower-cased haystacks, pack and stack membership and project states are computed once;
  a query is a linear scan with early exits, about a millisecond for 1000 skills.
- **Virtualized tables**: only visible rows are rendered; cells are produced lazily.
- **Cached preview**: SKILL.md is rendered by a small built-in Markdown renderer (first 16 KB), cached per skill and
  width.
- **Cached classification**: domain/stack regexes run at index time; `.cache/classify.json` is keyed by a hash of
  the taxonomy and every pack, so start-up only reads JSON (24 ms on 718 skills).
- **Nothing slow on the UI goroutine**: project status (tree hashing), upstream checks, staging, imports and
  installs run in commands that report progress; tabs other than Library load when first opened.
- `SKILLMAN_TRACE=<file>` logs the duration of every Update and View to check this on your machine.
