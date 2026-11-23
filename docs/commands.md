# Command reference

skillman is one binary. Run without arguments it opens the TUI; with a command it runs non-interactively. Every
command that reads data takes `--json`, and `skillman help <command>` prints its options.

| Entry point | What it is |
|---|---|
| `skillman [dir]` · `skillman tui [dir]` | The terminal UI (default project: git root of the current dir) |
| `skillman <command> …` | Everything else, scriptable |
| `skill-add …` | A link to the binary that keeps the old syntax: `skill-add [@pack] [name] … [dir]`, `--suggest`, `--packs`, `--list` |
| `skill-search <words>` | A link that prints `name [tags] description` like before |

Exit codes: `0` success, `1` a check failed or an item could not be processed, `2` usage error.

## Find skills

| Command | Description |
|---|---|
| `recommend [dir] [--stack ID]… [--task words] [--limit N] [--extra N] [--brief] [--json]` | Up to N local choices (default 5) with evidence, matching keywords, state and estimated added tokens. Repeated or comma-separated stack IDs select the recommendation scope and work in an empty project. Without stack evidence or a declaration, recommendations use the local `@core` pack. `--extra` controls the additional skills per detailed group. |
| `suggest [dir] [--json]` | Packs for the project's stack and the file that triggered each |
| `search <words…> [--domain D] [--stack S] [--limit N] [-C dir] [--json]` | Ranked search (exact name, name prefix, name, description, source) |
| `info <skill> [--body] [-C dir] [--json]` | Description, source and commit, domain/stacks, packs, tokens, validation findings, state in the project |
| `browse [--domain D] [--stack S] [-C dir] [--json]` | Skills grouped by domain › stack; ★ marks stacks of the project |
| `taxonomy [--default] [--json]` | The merged taxonomy; `--default` prints the built-in YAML |

Search syntax (TUI and CLI): plain words must all match · `-word` excludes · `tag:web` · `src:redis` · `pack:core` ·
`domain:backend` · `stack:vue` · `nopack` (in no stack pack) · `installed` · `notinstalled`.

## Project commands

`-C dir` sets the project (default `.`); most also accept the directory as a positional argument.

| Command | Description |
|---|---|
| `add <name\|@pack>… [dir] [--force] [--dry-run] [--json]` | Copy skills and whole packs into a project, link them, record them in the manifest. A pack member that is not in the library yet is reported as skipped; an explicitly named one fails |
| `remove <name\|@pack>…` | Remove skills or a pack to trash. Project manifest ownership determines which shared or manually added copies stay. For older manifests missing an association, re-add the pack you want to keep before removing the other one. |
| `status [dir] [--agents [--include-global]] [--json]` | State of every project skill. `--agents` lists on-disk locations for Claude Code, Codex, Cursor, GitHub Copilot CLI and OpenCode. Add `--include-global` to compare with their standard personal skill directories. |
| `doctor [dir] [--json]` | Links, drift, shadowing by global skills, description token budget, packs with missing skills |
| `diff <name>` | Unified diff library → project copy |
| `update [names] [--force] [--dry-run] [--json]` | Refresh `outdated` copies. Explicit names can include local edits or pins with `--force`. Use `--dry-run` to preview eligible and blocked updates without writing files. Exit 1 if any selected update is blocked. |
| `update-projects [--dry-run]` | Refresh outdated skills in every registered project |
| `adopt [names]` | Start tracking copies that were added before the manifest existed |
| `pin <names>` · `unpin <names>` | Keep a project copy as it is: skipped by `update`, accepted by `verify` |
| `repair-links [dir]` | Repair Claude links and copies. When only the project skill has changed, refresh the unchanged Claude copy. Reports separately edited copies and occupied paths for manual review. |
| `verify [dir] [--json]` | **CI check.** Requires a valid manifest, matching copies and a valid Claude link or mirror. Pinned edits are accepted when the layout is intact. Needs no library. |
| `audit [dir \| skill-dir] [--library] [--fail-on review\|error\|never] [-q] [--json]` | **Offline risk scan**: validation errors, hooks, pipe-to-shell, credential reads, broad `allowed-tools`, symlinks, executables |
| `projects [--json]` | Projects where skillman installed skills |

`add --dry-run` reports the requested action and reason for each skill, including blocked paths and
skipped pack members. A computed preview exits 0. Check its `ok` field before applying. `added_tokens`
counts eligible new or restored skills and `budget_after` also accounts for replacements.

### Project skill states

| State | Meaning | What to do |
|---|---|---|
| `ok` | Same content as the library | nothing |
| `outdated` | The library has a newer version, the project copy is untouched | `update` |
| `modified` | Edited in the project, the library did not change | keep and `pin`, or `update --force` |
| `conflict` | Edited in the project **and** changed in the library | `diff`, then decide |
| `untracked` | A folder in `.agents/skills` the manifest does not know | `adopt` |
| `missing` | In the manifest, gone from disk | `add <name>` or `update <name> --force` to restore from the library |
| `orphan` | Tracked, but the skill is no longer in the library | keep or `remove` |
| `invalid` | A symlink or file where a real directory should be | fix by hand |

## Packs

| Command | Description |
|---|---|
| `pack list [--vendor] [-v] [--json]` | Packs with resolved sizes (`-v` lists members) |
| `pack show <pack> [--json]` | Members (own and via `extends`), descriptions, token total |
| `pack create <pack> [skills…] [-d DESC] [--extends a,b]` | New pack |
| `pack add <pack> <skills…>` · `pack rm <pack> <skills…>` | Change members |
| `pack describe <pack> <text…>` · `pack extends <pack> [packs…]` | Change description / composites |
| `pack rename <old> <new>` · `pack delete <pack>` | Rename (updates `extends` elsewhere) / delete to trash |

Pack files are written exactly as before (PyYAML style), so editing them causes no noise in the library's git.

## Library commands

| Command | Description |
|---|---|
| `library check [skills…] [--pack P] [--log] [--json]` | Read-only upstream check for all, one pack, or chosen skills |
| `library auto-safe` | Apply only safe updates and commit them; risky ones wait in the queue |
| `library apply [skills…] [--allow-risky] [--no-commit]` | Apply every changed skill you selected |
| `library pending [--json]` | Risky updates waiting for review |
| `library rollback <skill>` | Restore the previous library version from git and pin it |
| `library pin <skills>` · `library unpin <skills>` | Freeze a library skill against upstream updates |
| `library new <skill> [-d DESC]` | Scaffold a local skill (no upstream; updates never touch it) |
| `library rebuild-index` · `library validate [--strict] [-v]` | Re-validate every skill, rewrite `INDEX.md/json` and `VALIDATION.md` |
| `library build-packs` | Regenerate `packs/vendor/*` (one pack per upstream owner with ≥ 3 skills) |
| `trash list \| restore <id> \| empty --yes [ids] [-C dir]` | Removed project copies and deleted packs. Use `-C dir` to include entries saved in that project's fallback trash. |
| `init` | Create or refresh the library: starter packs, `repos.txt`, the `skillman` skill, `.gitignore`, git |
| `import <source> [--list] [--skill name] [-C dir] [--dry-run] [--commit SHA] [--allow-risky] [--json]` | Accepts a GitHub slug, repository URL or skill directory URL. Lists choices for multi-skill sources. Repeat `--skill` to select several. The default destination is the library. Add `-C` to install into a project. Dry-run fetches into the cache and returns commit-pinned `apply_args`. |
| `bootstrap --pack core\|web-vue\|postgres [--dry-run] [--allow-risky] [--json]` | Preview pinned sources offline or import missing starter members into the library. Existing skills and upstream candidate selections are preserved. After inspecting audit findings, `--allow-risky` accepts review-level findings. Validation errors and missing licenses still block import. |
| `agent-setup [--dry-run] [--remove] [--no-claude] [--no-codex]` | Link the `skillman` skill into `~/.claude/skills` and `~/.agents/skills` |

## Import pipeline

| Command | Description |
|---|---|
| `upstream verify [repos.txt] [-j 8]` | Sparse, blob-less clone of each repo in `staging/repos.txt`. Refreshes `staging/CANDIDATES.md` and keeps selections at the same commit and folder. |
| `upstream candidates [words] [--ticked] [--new] [--json]` | List candidates with their line numbers |
| `upstream tick --pack core web-vue` | Tick the candidates a pack needs (one per name, licensed repos first) |
| `upstream tick --tier official --max-per-repo 40` · `--repo owner/name` · `--reset` | Tick in bulk |
| `upstream tick --line N … [--untick]` | Tick or untick specific candidates |
| `upstream import [--dry-run] [--update] [--rename-collisions] [--allow-risky]` | Import ticked candidates at the verified commit, audit their files and rebuild the index. Review-level findings require `--allow-risky`. A preview may use the cache. The library skills and import log are written on apply. |

## Compatibility aliases

`install` → `add`, `uninstall` → `remove`, `packs` → `pack list`, `expand <packs>`, `validate-packs`,
`update-library --check|--apply|--auto-safe [--pack P]`, `rebuild-index`, `rollback`, `lib-pin`, `lib-unpin`,
`new` → `library new`.

## TUI keys

Global: `1`–`8` or `[` `]` switch tabs · `tab`/`shift+tab` move between panes · `/` search or filter ·
`↑↓ j k PgUp PgDn g G` move · `space` select · `ctrl+a` select all shown · `esc` clear · `enter` open ·
`ctrl+p` command palette · `?` help · `ctrl+r` reload · `q` quit.

Mouse: click a tab, a row, the ● column (select) or a footer key; double-click a row to open it; the wheel scrolls
the pane under the pointer; drag the `│` divider in Library to resize the preview; click outside a dialog to close it.

| Tab | Keys |
|---|---|
| 1 Library | `i` install selection · `x` remove from project · `p` add to pack (or a new pack) · `enter` full SKILL.md · `w` warnings |
| 2 Browse | `enter`/`→` expand · `←` collapse · `i` install selection · `I` install the stack's pack · `n` new pack from selection (or everything shown) · `p` add to pack · `u` only not installed · `/` filter the group |
| 3 Packs | `i` install · `u` remove from project · `n` new · `a`/`x` add/remove skill · `e` description · `E` extends · `r` rename · `d` delete · `v` vendor packs |
| 4 Project | `u`/`U` update one/all · `D` diff · `x` remove · `P` remove pack · `a` adopt · `t` pin · `s` suggest · `o` doctor · `v` verify · `A` audit · `l` fix links · `c` change project |
| 5 Updates | `c` check all · `k` check a pack · `L` check the Library selection · `enter` changelog + diff · `a` apply selected · `A` apply all safe · `r` rollback · `f` only changes · `p` update projects |
| 6 Upstream | `space` tick · `I` preview import (Enter to continue to confirmation) · `t` only ticked · `V` verify repos · `/` filter |
| 7 Validation | `/` filter · `enter` open in Library · `v` re-validate |
| 8 Trash | `R` restore · `D` diff vs library · `x` delete forever · `E` empty |
