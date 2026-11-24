# Changelog

## Unreleased

### Added
- Stack detection for Rust (`Cargo.toml`), PHP (`composer.json`), Ruby (`Gemfile`, `*.gemspec`) and Swift
  (`Package.swift`), next to Go. `recommend` suggests library skills classified under those stacks.
- `status --json` reports `matches_manifest` (the copy against the committed manifest, which `verify` checks) and
  `library` (`same`, `differs` or `absent` on this machine). The text output notes when the two disagree.
- `recommend --json` reports `install_tokens` for the suggested install and warns when that total exceeds the
  budget, not only when the top choice does.
- A labelled recommendation test set and a ranking benchmark (`docs/selection-evaluation.md`).

### Fixed
- Skill hashes are the same on Windows, Linux and macOS: symlink targets are hashed with forward slashes, import
  checkouts keep LF line endings, and every project gets the `-text` `.gitattributes` files, not only copy-mode
  projects. A manifest written on Windows for a skill that contains a symlink shows the skill as changed once; run
  `skillman update` or pin it again.
- On Windows, replacing an existing `.claude/skills` symlink created a file link that clients could not open as a
  folder. The new link is now created next to the old one.
- A symlink-mode project checked out by Git for Windows defaults (CRLF, links as plain files) can be converted
  with `skillman repair-links --mode copy`. When Git reports a skill as clean and its committed bytes match the
  manifest, the conversion restores those bytes; real local edits are still refused. `verify` points to it.
- Saving a preference no longer replaces an invalid `settings.json` with only that preference; it reports the
  file instead.

### Changed
- `recommend --task` also ranks library skills outside the detected stacks (evidence `library`). A stack match
  still wins over an equally good match elsewhere, declared `--stack` IDs keep the scope closed, and filler
  words such as "for" or "the" no longer count as matches.
- `add`, `update` and `adopt` refuse a skill that contains symlinks and name the links. Git for Windows checks
  them out as plain files by default, so the committed copy would fail `verify` there. Replace the links with
  regular files in the library skill. Imports already inline or drop upstream symlinks. They also refuse a skill
  whose own `.gitattributes` turns on line-ending or filter conversion, which would override the project's `-text`.
- A task made only of filler words is treated as no task.
- `docs/ci.md` states what `verify` checks: files against the committed manifest, not who changed both.

## 0.2.0 — Go rewrite

skillman is now a single Go binary (Bubble Tea v2 TUI + CLI). Library, packs, project manifests, trash and registry
are unchanged on disk; run `./install.sh` again to switch.

### Added
- **Browse tab** and `skillman browse`: every skill filed under domain › stack (16 domains, ~70 stacks), stacks used
  by the project marked ★, install a stack's pack with `I`, save a selection as a pack with `n`. Overrides and new
  domains in `$SKILL_LIBRARY/taxonomy.yaml`; `skillman taxonomy`.
- **Agent-first CLI**: `recommend`, `search`, `info`, `browse`, `add --dry-run` and every read command with `--json`;
  pack editing from the CLI (`pack create|add|rm|describe|extends|rename|delete|show|list`); `library pending`;
  `upstream candidates|tick --line`; consistent exit codes (0 ok, 1 failed, 2 usage).
- `skillman agent-setup` (run by `install.sh`): links the `skillman` skill into `~/.claude/skills` and
  `~/.agents/skills` so Claude Code, Codex and OpenCode can use skillman in any project.
- `skillman init`: bootstraps a library from data embedded in the binary.
- TUI: mouse everywhere (tabs, rows, ● selection, double-click, wheel per pane, draggable preview divider, clickable
  footer and dialog buttons), command palette (`ctrl+p`), help overlay (`?`), ranked search with `-word`,
  `domain:`, `stack:`, `notinstalled`; "new pack from selection".
- `SKILLMAN_TRACE=<file>` to measure frame times.

### Changed
- Much faster on a 718-skill library: start-up 1.3 s → 24 ms, keystroke in search 80 ms → 1.1 ms, cursor move
  413 ms → 1.5 ms (see docs/architecture.md, Performance design).
- Commands are `skillman <cmd>` instead of `python3 …/scripts/skillcore.py <cmd>`; the old names are aliases.
- Installing a pack whose members are not all in the library reports the missing ones as skipped instead of failing.
- Importing an upstream skill whose frontmatter name differs now rewrites only the `name:` line.
- `doctor` no longer reports the globally installed `skillman` skill as shadowing.
- The weekly timer moved to `contrib/systemd` and calls `skillman library auto-safe`.

### Removed
- The Python implementation (`scripts/*.py`, `bin/*`, `tests/*.py`) and the uv/Textual requirement.

## 0.1.x (unreleased additions)

### Added
- `skillcore.py verify`: a frozen-manifest check for CI. It fails when committed skills drift from their recorded
  hashes, and needs no library.
- `skillcore.py audit`: an offline risk scan of a project, one skill or the whole library, with
  `--fail-on review|error|never` and `--json`.
- `skillcore.py new`: scaffold a local skill in the library.
- TUI Project tab: `v` runs `verify`, `A` runs `audit`.
- `skills/skillman`: a skill that teaches agents to manage skills through skillman. `install.sh` adds it to the
  library, and `skill-add skillman` puts it in a project.
- Documentation in `docs/` (commands, packs, updates, CI, architecture), plus `CONTRIBUTING.md` and `SECURITY.md`.

### Changed
- The screenshot generator moved to `tools/make_screenshots.py`.

## 0.1.0 — 2026-11-21

- First public release: packs with `extends`, per-project injection with a manifest, drift states, trash, upstream
  updates by tree OID with risk gates and rollback, a 7-tab Textual TUI, and an offline test suite.
