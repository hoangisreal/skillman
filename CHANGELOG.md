# Changelog

## Unreleased

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
