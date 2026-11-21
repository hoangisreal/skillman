---
name: skillman
description: Add, inspect, update or remove agent skills in this project through skillman (skill-add, skillcore.py) instead of copying SKILL.md folders by hand. Use when the user asks which skills a project has, wants a skill or pack added, or asks to check skill drift, health, security or upstream updates.
---

# Managing skills with skillman

Skills live in a cold library (`$SKILL_LIBRARY`, default `~/skill-library`) that no agent scans. A project gets
real copies in `.agents/skills/<name>/`, a link in `.claude/skills/<name>`, and a record in
`.agents/skill-manifest.json`. Always go through the tools so the manifest stays correct.

`CORE` below means `python3 "${SKILL_LIBRARY:-$HOME/skill-library}/scripts/skillcore.py"`.

## Find and add
1. `skill-add --suggest .` proposes packs from the project's manifests (package.json, pyproject.toml, go.mod, ...).
2. `skill-add --packs` lists packs; `skill-search <words>` searches skill names and descriptions.
3. `skill-add @pack name …` installs into the current project. Prefer a few relevant skills over whole stacks:
   each skill's description costs context in every session.

## Inspect
- `CORE status .` shows each skill's state: ok, outdated, modified, conflict, untracked, missing, orphan.
- `CORE doctor .` checks links, shadowing by global skills and the description token budget.
- `CORE audit .` is an offline risk scan (hooks, download-piped-to-shell, broad `allowed-tools`, symlinks, executables).
- `CORE verify .` confirms the committed copies match the manifest. It is the CI check, and needs no library.

## Change
- Refresh outdated copies: `CORE update -C . [names]`. Show the diff first: `CORE diff -C . <name>`.
- Keep a deliberate local edit: `CORE pin -C . <name>`.
- Remove: `CORE uninstall -C . <name|@pack>`. Removed copies go to a trash (`CORE trash list`).
- Track copies that predate the manifest: `CORE adopt -C .`.
- New project-specific skill: `CORE new <name> -d "<description>"`, edit it, then `skill-add <name>`.

## Rules
- Never copy, edit or delete files under the library's `skills/` by hand, and never point an agent at the library.
- Upstream updates of the library are the user's call. `CORE update-library --check` is read-only and safe.
  `--apply`, `--auto-safe` and `--allow-risky` change the library, so ask the user before running them.
- Report `audit` review findings to the user rather than "fixing" a third-party skill silently.
- Commit `.agents/skills`, `.claude/skills` and `.agents/skill-manifest.json` together, with the user's consent.
