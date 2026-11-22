---
name: skillman
description: Select and manage project skills from the user's local Skillman library. Use for stack-based recommendations, project packs, installation previews, and tracked updates.
---

# Managing project skills with skillman

Skills live in a cold library (`$SKILL_LIBRARY`, default `~/skill-library`) that no agent scans. A project gets
real copies in `.agents/skills/<name>/` (Codex, OpenCode), a link in `.claude/skills/<name>` (Claude Code) and a
record in `.agents/skill-manifest.json`. Always go through `skillman` so the manifest stays correct.
Every command is non-interactive; add `--json` to read results reliably. `skillman help <command>` shows options.

## Choose skills for this project
1. Run `skillman recommend -C <project> --json` for local recommendations. For a particular task, add
   `--task "vue test"` or other keywords in the catalog's language. Start with `shortlist` (five by default).
   Show each choice's evidence, reason, source, state and estimated added description tokens. Use `groups` for alternatives.
2. Refine when needed:
   - `skillman browse --json` — every domain › stack with its skills; `skillman browse --stack postgres`
   - `skillman search "<words>" --json` — ranked; filters: `stack:vue domain:backend pack:core src:owner notinstalled`
   - `skillman info <skill> --json` (add `--body` to read the SKILL.md) before adding anything unfamiliar.
3. Propose a short list to the user: name, why, token cost. Prefer a pack's curated skills plus a few specific
   extras over whole domains: every skill's description costs context in every session (budget ≈ 2000 tokens).
4. Preview the chosen names with `skillman add -C <project> <names…> --dry-run --json`. Keep the absolute
   project path from the recommendation; `install_args` is an argv array for the proposed new skills.
   After the user selects skills, install those names and report the actual summary. Confirm with
   `skillman status -C <project> --json` and `skillman verify -C <project> --json`.

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
