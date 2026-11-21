# Using skillman from coding agents

skillman is built to be driven by Claude Code, Codex CLI and OpenCode as well as by hand: every TUI action is a
non-interactive command, read commands print stable JSON with `--json`, and exit codes are `0` ok, `1` failed
check or item, `2` usage error.

## Setup

`install.sh` (or `skillman agent-setup`) links the [`skillman` skill](../skills/skillman/SKILL.md) into:

| Directory | Read by |
|---|---|
| `~/.claude/skills/skillman` | Claude Code (and OpenCode, which also reads Claude's skill folders) |
| `~/.agents/skills/skillman` | Codex CLI |

It is a symlink to the copy in your library, so `skillman init` refreshes it. It costs one short description in
each session. `skillman agent-setup --remove` undoes it; a real directory at those paths is never touched. To keep
it per project only, skip agent-setup and run `skill-add skillman` in the project.

## The workflow the skill teaches

```bash
skillman recommend . --json
```

```json
{
  "project": "/home/me/code/shop",
  "groups": [
    { "pack": "postgres", "reason": "package.json", "description": "PostgreSQL design, migrations, search, tuning",
      "stacks": ["postgres"], "pack_installed": false,
      "skills": [
        { "name": "postgres-best-practices", "description": "…", "source": "neondatabase/postgres-skills",
          "in_pack": true, "in_library": true, "state": "", "tokens": 52 },
        { "name": "connect-cdc-postgres", "in_pack": false, "in_library": true, "tokens": 61, "…": "…" } ] } ],
  "budget": { "total": 410, "budget": 2000, "…": "…" },
  "install_hint": "skillman add @core @web-vue @postgres @docker"
}
```

Then, as needed: `skillman browse --stack postgres --json`, `skillman search "postgres migration" --json`,
`skillman info <skill> --json` (add `--body` to read it). The agent proposes a short list with token costs, checks it
with `skillman add … --dry-run --json`, installs after the user agrees, and confirms with `status --json` and
`doctor --json`.

## JSON shapes

| Command | Output |
|---|---|
| `add`, `remove`, `update`, `adopt`, `pin`, `verify`, `library pin` | `{"ok": bool, "results": [{"name", "ok", "message"}]}` |
| `add --dry-run` | `{"project", "skills": [{"name", "pack", "state", "in_library", "tokens"}], "added_tokens", "budget_after", "budget"}` |
| `status` | `[{"name", "packs", "pinned", "manual", "tracked", "in_library", "link_ok", "state", "same_as_library"?}]` |
| `doctor` | `{"project", "ok", "checks": [{"level": "ok\|warn\|error", "message"}]}` |
| `search` | `{"query", "count", "skills": [{"name", "description", "source", "domain", "stacks", "packs", "state"?}]}` |
| `info` | `{"name", "description", "source", "commit", "domain", "stacks", "tags", "packs", "tokens", "lines", "errors", "warnings", "state", "body"?}` |
| `browse` | `{"project", "domains": [{"id", "title", "skills", "stacks": [{"id", "title", "packs", "skills", "detected"}], "other"}]}` |
| `audit` | `[{"name", "path", "level": "ok\|info\|review\|error", "errors", "review", "executables"}]` |
| `library check` | `{"items": [{"name", "repo", "status", "commit", "new_commit", …}], "no_upstream", "errors", "staged", "results"}` |
| `pack list` / `pack show` | `{"packs": [{"name", "description", "extends", "skills", "count"}]}` / `{"pack", "skills", "tokens", "install"}` |

## Guardrails

The skill tells agents to ask before adding more than a handful of skills, before changing packs or the library, and
before committing; `library check` is read-only, while `library apply`, `auto-safe` and `--allow-risky` change the
library. skillman itself refuses unsafe writes regardless of who calls it: symlinked `.agents`/`.claude` folders,
real directories where a link belongs, path traversal in names, and a dirty library working tree.
