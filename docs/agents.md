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

### Cursor and GitHub Copilot CLI

Both clients discover project skills under `.agents/skills` and personal skills under `~/.agents/skills`. Run `skillman agent-setup` to expose the Skillman instructions. Open the project in Cursor and check its Skills settings, or run `/skills reload` and inspect `/skills` in Copilot CLI. Ask “Trong thư viện local, skill nào phù hợp để test component Vue? Chưa cài gì.” Check that the recommendation targets this project and that only the selected name is passed to `add` after selection. [Cursor skills](https://cursor.com/docs/skills) · [Copilot CLI skills](https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-skills)

## The workflow the skill teaches

Ask the agent to select skills **from your local Skillman library for this project**. For explicit invocation use
`$skillman` in Codex or `/skillman` in Claude Code. External discovery tools such as find-skills can research
public sources. Personal Codex installs use skill-installer. For an external skill approved for a managed project,
import its source into the library and install the project copy through Skillman.

```bash
skillman recommend -C ./shop --task "postgres migration" --brief --json
```

The response contains the resolved project, budget, shortlist and `install_args`. Each choice includes its
source, stack evidence file, matched keywords, project state and additional description tokens. With no task,
directly detected stacks each get a turn. Omit `--brief` to inspect the detailed groups.
For a new project with a chosen stack, pass `--stack vue --stack postgres` or `--stack vue,postgres`.
The evidence for an explicit choice is `declared:<id>`. When the stack is open, preview the local `@core` pack.

Then, as needed: `skillman browse --stack postgres --json`, `skillman search "postgres migration" --json`,
`skillman info <skill> --json` (add `--body` to read it). The agent proposes a short list with token costs, checks it
with `skillman add -C <project> <selected names> --dry-run --json`, installs the selected names, and confirms with
`status -C <project> --json` and `verify -C <project> --json`. Pass `install_args` directly as arguments when the
whole proposed selection is wanted. These arguments include missing tracked copies that can be restored.
An empty array means every shortlisted copy is present or the shortlist is empty.
For an untracked copy, inspect `diff` before `adopt`. When the library needs content, preview the pinned sources
with `bootstrap --pack core --dry-run --json`. For a supplied GitHub source, list the skills and preview the selected name.

```bash
skillman import vuejs-ai/skills --list --json
skillman import vuejs-ai/skills --skill vue-testing-best-practices -C ./shop --dry-run --json
```

Pass the returned `apply_args` to the CLI to reuse the reviewed SHA. Imports check the license and audit
the staged skill. Existing names can be reused only with matching source, version and content. A failed project
installation leaves the successful library import available and returns `retry_args` for `add`.

## JSON shapes

| Command | Output |
|---|---|
| `remove`, `update`, `adopt`, `pin`, `verify`, `library pin` | `{"ok": bool, "results": [{"name", "ok", "message"}]}` |
| `add` | Same result fields plus `summary` with `installed`, `already_present`, `skipped`, `failed`. Exit 1 when every requested member is unavailable. |
| `add --dry-run` | `{"project", "ok", "skills": [{"name", "pack", "state", "in_library", "tokens", "action", "reason"}], "added_tokens", "budget_after", "budget"}` |
| `status` | `[{"name", "packs", "pinned", "manual", "tracked", "in_library", "link_ok", "state", "same_as_library"?}]` |
| `status --agents` | `{"project", "include_global", "agents": [{"id", "roots", "skills", "duplicates"}]}`. Skill rows include `name`, `path`, `scope`, `tracked`, `state` and optional `aliases`. |

For agent status, `available` describes a skill on disk. The scanner checks direct skill folders under the
documented project roots. `--include-global` enables the corresponding standard personal roots. Symlinks
outside those roots appear as `external_link`. Shared locations and matching managed Claude mirrors appear
once per client with alternate paths in `aliases`. Separate copies with the same skill name appear in `duplicates`.
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
