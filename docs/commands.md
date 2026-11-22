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
| `recommend [dir] [--task words] [--limit N] [--extra N] [--json]` | Up to N local choices (default 5), each with stack evidence, matching task keywords, source, state and estimated added description tokens. Detailed groups include curated members plus `--extra` additional skills per stack. Install arguments include the target directory. |
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
| `remove <name\|@pack>…` | Remove skills or a pack (skills still needed by another pack or added manually stay). Goes to trash |
| `status [dir] [--json]` | State of every project skill (see [states](#project-skill-states)) |
| `doctor [dir] [--json]` | Links, drift, shadowing by global skills, description token budget, packs with missing skills |
| `diff <name>` | Unified diff library → project copy |
| `update [names] [--force]` | Refresh `outdated` copies (`--force` also replaces `modified`) |
| `update-projects [--dry-run]` | Refresh outdated skills in every registered project |
| `adopt [names]` | Start tracking copies that were added before the manifest existed |
| `pin <names>` · `unpin <names>` | Keep a project copy as it is: skipped by `update`, accepted by `verify` |
| `repair-links [dir]` | Recreate missing or broken `.claude/skills` links |
| `verify [dir] [--json]` | **CI check.** Exit 1 unless the committed copies match the manifest hashes. Needs no library |
| `audit [dir \| skill-dir] [--library] [--fail-on review\|error\|never] [-q] [--json]` | **Offline risk scan**: validation errors, hooks, pipe-to-shell, credential reads, broad `allowed-tools`, symlinks, executables |
| `projects [--json]` | Projects where skillman installed skills |

### Project skill states

| State | Meaning | What to do |
|---|---|---|
| `ok` | Same content as the library | nothing |
| `outdated` | The library has a newer version, the project copy is untouched | `update` |
| `modified` | Edited in the project, the library did not change | keep and `pin`, or `update --force` |
| `conflict` | Edited in the project **and** changed in the library | `diff`, then decide |
| `untracked` | A folder in `.agents/skills` the manifest does not know | `adopt` |
| `missing` | In the manifest, gone from disk | `update --force` or `remove` |
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
| `trash list \| restore <id> \| empty --yes [ids]` | Removed project copies and deleted packs |
| `init` | Create or refresh the library: starter packs, `repos.txt`, the `skillman` skill, `.gitignore`, git |
| `bootstrap --pack core\|web-vue\|postgres [--dry-run] [--json]` | Preview pinned sources offline or import missing starter members into the library. Existing skills and upstream candidate selections are preserved. License or audit failures are reported per skill. Rerun to retry missing members. |
| `agent-setup [--dry-run] [--remove] [--no-claude] [--no-codex]` | Link the `skillman` skill into `~/.claude/skills` and `~/.agents/skills` |

## Import pipeline

| Command | Description |
|---|---|
| `upstream verify [repos.txt] [-j 8]` | Sparse, blob-less clone of each repo in `staging/repos.txt`; writes `staging/CANDIDATES.md` |
| `upstream candidates [words] [--ticked] [--new] [--json]` | List candidates with their line numbers |
| `upstream tick --pack core web-vue` | Tick the candidates a pack needs (one per name, licensed repos first) |
| `upstream tick --tier official --max-per-repo 40` · `--repo owner/name` · `--reset` | Tick in bulk |
| `upstream tick --line N … [--untick]` | Tick or untick specific candidates |
| `upstream import [--dry-run] [--update] [--rename-collisions]` | Import ticked candidates at a pinned commit through the safety pipeline, then rebuild the index |

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
| 6 Upstream | `space` tick · `I` import (dry run, then confirm) · `t` only ticked · `V` verify repos · `/` filter |
| 7 Validation | `/` filter · `enter` open in Library · `v` re-validate |
| 8 Trash | `R` restore · `D` diff vs library · `x` delete forever · `E` empty |
