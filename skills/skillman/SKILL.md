---
name: skillman
description: Choose, add, inspect, update or remove agent skills for the current project with the skillman CLI instead of copying SKILL.md folders by hand. Use when the user asks which skills a project has or needs, wants skills for a stack (Vue, Postgres, Docker, ML, …), or asks about skill drift, health, security or upstream updates.
---

# Managing project skills with skillman

Skills live in a cold library (`$SKILL_LIBRARY`, default `~/skill-library`) that no agent scans. A project gets
real copies in `.agents/skills/<name>/` (Codex, OpenCode), a link in `.claude/skills/<name>` (Claude Code) and a
record in `.agents/skill-manifest.json`. Always go through `skillman` so the manifest stays correct.
Every command is non-interactive; add `--json` to read results reliably. `skillman help <command>` shows options.

## Choose skills for this project
1. `skillman recommend . --json` — detected stacks (from package.json, pyproject.toml, go.mod, Dockerfile, …) as
   `groups`: each has a `pack`, its curated `skills` (`in_pack: true`), extra same-stack skills (`in_pack: false`),
   the project `state` of each skill and its `tokens`. `install_hint` is the one-line install.
2. Refine when needed:
   - `skillman browse --json` — every domain › stack with its skills; `skillman browse --stack postgres`
   - `skillman search "<words>" --json` — ranked; filters: `stack:vue domain:backend pack:core src:owner notinstalled`
   - `skillman info <skill> --json` (add `--body` to read the SKILL.md) before adding anything unfamiliar.
3. Propose a short list to the user: name, why, token cost. Prefer a pack's curated skills plus a few specific
   extras over whole domains: every skill's description costs context in every session (budget ≈ 2000 tokens).
4. `skillman add @pack skill … --dry-run` shows what would be copied and the token total; then run it without
   `--dry-run` once the user agrees.

## Inspect
- `skillman status --json` — state per skill: ok, outdated, modified, conflict, untracked, missing, orphan.
- `skillman doctor --json` — links, shadowing by global skills, token budget, packs with missing skills.
- `skillman audit . --json` — offline risk scan (hooks, download-piped-to-shell, credential reads, broad
  `allowed-tools`, symlinks, executables). `skillman verify .` is the CI check (needs no library).

## Change
- Refresh outdated copies: `skillman update [skills…]`; show the change first with `skillman diff <skill>`.
- Keep a deliberate local edit: `skillman pin <skill>`. Remove: `skillman remove <skill|@pack>` (to trash,
  `skillman trash list`). Track pre-existing copies: `skillman adopt`.
- Group skills for reuse: `skillman pack create <name> <skills…> -d "<description>"`, `pack add|rm <pack> <skills…>`.
- A project-specific skill: `skillman library new <name> -d "<description>"`, edit it, then `skillman add <name>`.

## Rules
- Ask the user before `add`ing more than a handful of skills, before changing packs or the library, and before
  committing. Never copy, edit or delete files under the library's `skills/` by hand; never point an agent at it.
- Upstream updates are the user's call. `skillman library check --json` is read-only and safe; `library apply`,
  `library auto-safe` and `--allow-risky` change the library: ask first.
- Report `audit` findings with level `review`/`error` to the user instead of silently "fixing" third-party skills.
- Commit `.agents/skills`, `.claude/skills` and `.agents/skill-manifest.json` together, with the user's consent.
