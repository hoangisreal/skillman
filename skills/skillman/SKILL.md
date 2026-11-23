---
name: skillman
description: Select and manage project skills from the user's local Skillman library. Use for stack-based recommendations, project packs, importing a chosen GitHub source, installation previews, and tracked updates.
---

# Managing project skills with skillman

Skills live in a cold library (`$SKILL_LIBRARY`, default `~/skill-library`) that no agent scans. A project gets
real copies in `.agents/skills/<name>/` (Codex, OpenCode), a link in `.claude/skills/<name>` (Claude Code) and a
record in `.agents/skill-manifest.json`. Always go through `skillman` so the manifest stays correct.
Every command is non-interactive; add `--json` to read results reliably. `skillman help <command>` shows options.

## Choose skills for this project
Start with one call to **`skillman recommend -C <project> --brief --json`**. For a specific task, add
`--task "<keywords>"`. Translate Vietnamese requests into a few catalog keywords, such as `vue test`
or `postgres migration`. Omit the task for a cross-stack selection.

Show the shortlisted names with their source, stack evidence file, matched keywords, project state
and added tokens. Use `info <name> --json` only when a decision needs more detail. If the shortlist is
empty, say which capability is missing locally.

After the user chooses, run `skillman add -C <project> <chosen names> --dry-run --json` using the exact
project path returned. Install those names with the same arguments without `--dry-run` and report the
actual results. Check `status -C <project> --json` and `verify -C <project> --json`.

## Import a supplied GitHub source

Use `skillman import <source> --list --json` to read names, directories and descriptions. Preview the
selection with `--skill <name> --dry-run --json`, repeating `--skill` for multiple choices. Include
`-C <project>` when a project copy is requested. With approval, pass `apply_args` as arguments to
`skillman` to import the reviewed commit. Report library and project outcomes separately. If project
installation failed, use `retry_args` after resolving the reported cause.

## Choosing the tool
- “Which skills from my library suit this repo?” uses Skillman. Local recommendations work offline.
- “Find a better skill on GitHub” calls for external discovery (e.g. find-skills). If a source is chosen for a
  Skillman-managed project, import it into the library first and use Skillman to track its installation.
- Personal Codex skill installation belongs to its skill-installer. An ordinary request to write code or tests
  uses the agent's existing capabilities.
- If the local shortlist is empty, explain what is missing. For a starter pack, show
  `skillman bootstrap --pack core --dry-run --json` (also web-vue or postgres). Import sources on request.
- For an untracked project copy, show its status and diff before offering to adopt it.
- Explicit selection: `$skillman` in Codex CLI or `/skillman` in Claude Code.

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
