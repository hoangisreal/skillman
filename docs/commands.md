# Command reference

skillman has three entry points that share one core module:

| Entry point | What it is |
|---|---|
| `skill-add` | Day-to-day installer: `skill-add [@pack] [name] … [dir]` |
| `skill-search` | Grep over the library index: `skill-search postgres migration` |
| `skillman [dir]` | The Textual TUI (runs through `uv run --script`) |
| `skillcore.py <command>` | Everything else, scriptable: `python3 ~/skill-library/scripts/skillcore.py` |

`CORE` below is short for `python3 ~/skill-library/scripts/skillcore.py`. Every command takes `-h`.

## Project commands

| Command | Description |
|---|---|
| `skill-add @core @web-vue vitest [dir]` | Copy skills and whole packs into a project, link them, record them in the manifest |
| `skill-add --suggest .` | Read the project's manifests and propose packs |
| `skill-add --packs` · `skill-add --list` | List packs, list library skills |
| `CORE status [dir] [--json]` | State of every project skill (see [states](#project-skill-states)) |
| `CORE doctor [dir]` | Links, manifest drift, shadowing by global skills, description token budget |
| `CORE update -C dir [names] [--force]` | Refresh `outdated` copies from the library (`--force` also replaces `modified`) |
| `CORE diff -C dir <name>` | Unified diff library → project copy |
| `CORE uninstall -C dir <name> @<pack>` | Remove skills or a pack (skills shared with another pack stay). Goes to trash |
| `CORE adopt -C dir [names]` | Start tracking copies that were added before the manifest existed |
| `CORE pin -C dir <names>` · `unpin` | Keep a project copy as it is: skipped by `update`, accepted by `verify` |
| `CORE repair-links [dir]` | Recreate missing or broken `.claude/skills` links |
| `CORE verify [dir]` | **CI check.** Exit 1 unless the committed copies match the manifest hashes. Needs no library |
| `CORE audit [dir \| skill-dir] [--library] [--fail-on review\|error\|never] [-q] [--json]` | **Offline risk scan**: validation errors, hooks, pipe-to-shell, credential reads, broad `allowed-tools`, symlinks, executables |

### Project skill states

| State | Meaning | What to do |
|---|---|---|
| `ok` | Same content as the library | nothing |
| `outdated` | The library has a newer version, the project copy is untouched | `update` |
| `modified` | Edited in the project, the library did not change | keep and `pin`, or `update --force` |
| `conflict` | Edited in the project **and** changed in the library | `diff`, then decide |
| `untracked` | A folder in `.agents/skills` the manifest does not know | `adopt` |
| `missing` | In the manifest, gone from disk | `update --force` or `uninstall` |
| `orphan` | Tracked, but the skill is no longer in the library | keep or `uninstall` |
| `invalid` | A symlink or file where a real directory should be | fix by hand |

## Library commands

| Command | Description |
|---|---|
| `CORE new <name> [-d "description"]` | Scaffold a local skill in the library (no upstream; updates never touch it) |
| `CORE packs [-v]` · `CORE expand @pack` | List packs with sizes, print a pack's resolved skills |
| `CORE validate-packs` | Packs that reference missing skills, and pack coverage |
| `CORE rebuild-index` | Re-validate every skill, rewrite `INDEX.md/json` and `VALIDATION.md` |
| `CORE update-library --check [--pack P] [names] [--log]` | Read-only upstream check for all, one pack, or chosen skills |
| `CORE update-library --auto-safe` | Apply only safe updates and commit them; risky ones wait in the queue |
| `CORE update-library --apply [--allow-risky] [--no-commit]` | Apply every change you selected |
| `CORE update-projects [--dry-run]` | Refresh outdated skills in every registered project |
| `CORE rollback <name>` | Restore the previous library version from git and pin it |
| `CORE lib-pin <names>` · `lib-unpin` | Freeze a library skill against upstream updates |
| `CORE trash list \| restore <id> \| empty --yes` | Removed project copies and deleted packs |

## Import pipeline

| Script | Description |
|---|---|
| `scripts/verify-repos.py [-j 8]` | Sparse, blob-less clone of each repo in `staging/repos.txt`; writes `staging/CANDIDATES.md` |
| `scripts/tick-candidates.py --pack core web-vue` | Tick the candidates a pack needs (one per name, licensed repos first) |
| `scripts/tick-candidates.py --tier official --max-per-repo 40` | Tick in bulk |
| `scripts/import-skills.py [--dry-run]` | Import ticked candidates at a pinned commit through the safety pipeline |
| `scripts/validate-library.py [-v] [--strict]` | Validate everything; non-zero on errors (or warnings with `--strict`) |
| `scripts/build-packs.py [--force] [--vendors-only]` | Regenerate `packs/vendor/*`; create missing stack packs |

## TUI keys

Global: `1`–`7` switch tabs · `/` search · `space` multi-select · `ctrl+p` command palette · `q` quit.

| Tab | Keys |
|---|---|
| Library | Markdown preview follows the cursor · `i` install selection · `p` add to pack · `w` show warnings |
| Packs | `i` install · `u` remove from project · `n` new · `r` rename · `e` description · `E` extends · `a`/`x` add/remove skill · `d` delete · `v` show vendor packs |
| Project | `u`/`U` update one/all · `D` diff · `x` remove · `P` remove pack · `a` adopt · `t` pin · `l` fix links · `s` suggest · `o` doctor · `v` verify · `A` audit · `c` change project |
| Updates | `c` check all · `k` check a pack · `L` check the Library selection · `enter` changelog + diff · `a` apply selected · `A` apply all safe · `r` rollback · `f` only changes · `p` update projects |
| Upstream | `space` tick · `t` only ticked · `I` import (dry-run first) |
| Validation | `enter` open in Library · `v` re-validate |
| Trash | `R` restore · `D` diff vs library · `x` delete forever · `E` empty |

`ctrl+r` reloads everything from disk.

## Environment

| Variable | Default | Effect |
|---|---|---|
| `SKILL_LIBRARY` | `~/skill-library` | Library root |
| `SKILLMAN_STATE` | `~/.local/state/skillman` | Trash and project registry (falls back to `<project>/.agents/.skill-trash` if not writable) |
| `SKILL_TOKEN_BUDGET` | `2000` | Description-token budget `doctor` warns about |
| `SKILLCORE_GIT_BASE` | `https://github.com/` | Upstream URL prefix (tests point it at `file://` bare repos) |
