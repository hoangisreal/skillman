# Upstream updates and safety gates

Skills come from third-party repositories, and a skill is a prompt plus whatever scripts sit next to it, so pulling
the latest version means running someone else's code inside your agent. skillman treats an update as a reviewable
change, not a sync.

## Provenance

Every imported skill carries a `SOURCE.json`:

```json
{
  "repo": "trailofbits/skills",
  "subpath": "plugins/semgrep/skills/semgrep",
  "commit": "3f1c…",
  "fetched_at": "2026-11-21T09:12:03+0700",
  "tier": "official",
  "license_review_required": true,
  "local_modifications": ["inlined 1 in-repo symlink(s): reference.md"]
}
```

`pinned: true` is added by `lib-pin` or `rollback`.

Skills you write yourself (`skillcore.py new`) and migrated local skills have no `repo`, and update checks list them
as `no upstream`.

## Checking

`update-library --check` groups skills by repository and does the least work it can:

1. One `git ls-remote` per repository. If `HEAD` has not moved, every skill from that repo is `up-to-date`.
2. If it moved, a blob-less fetch (`--filter=blob:none`, depth 1) into a persistent cache in
   `~/skill-library/.cache/repos` downloads commits and trees, not file contents.
3. For each skill, the tree object IDs of `<old>:<subpath>` and `<new>:<subpath>` are compared. Equal IDs mean equal
   content, so a repository that changed somewhere else costs nothing.

That is how ~600 skills across 60+ repositories can be checked in a few seconds. Results per skill:

| Status | Meaning |
|---|---|
| `up-to-date` | Same tree ID |
| `changed` | The subpath's content changed |
| `removed-upstream` | The subpath no longer exists at the new commit |
| `license-gone` | The repository no longer has a root license |
| `pinned` | Frozen with `lib-pin` or by a `rollback`, so not offered |

Scope it with `--pack web-vue` or by naming skills. `--log` prints the upstream commits that touched each subpath.

## Assessing

A changed skill is **materialized** at the new commit through the same pipeline as a first import:

- a license is required, in the skill folder or at the repository root (copied to `UPSTREAM-LICENSE.txt`);
- `SKILL.md` must be a real file, not a symlink;
- plugin wiring is dropped: `hooks/`, `commands/`, subagent definitions in `agents/*.md`, `.mcp.json`,
  `plugin.json`, `marketplace.json`, `.claude-plugin/`, `.codex-plugin/`, `node_modules/`;
- symlinks are inlined only if they resolve inside the repository; absolute or escaping links are dropped, never
  followed to your filesystem;
- recorded mechanical fixes (frontmatter `name`, a license comment above the frontmatter) are re-applied.

The staged copy is validated and compared with the current one. The update is **risky** if any of these is new:

| Signal | Example |
|---|---|
| Frontmatter registers hooks | `hooks:` in `SKILL.md` |
| Download piped to a shell | `curl … \| bash`, `wget … \| sh` |
| Encoded payloads | `base64 -d \| sh`, `eval(atob(…))` |
| Credential access | reads `~/.ssh`, `~/.aws`, `~/.kube`, `.env` |
| Prompt-injection phrasing | "ignore previous instructions" |
| Reverse shells | `nc -e`, `ncat -e` |
| Broader tools | `allowed-tools: Bash` or `Bash(*)` |
| New executable or script file | `scripts/install.sh` appeared |
| Validation errors | broken frontmatter, name mismatch |
| A manual local change would be lost | an entry in `local_modifications` that cannot be re-applied |

The same checks run offline over any project or the whole library with `skillcore.py audit`.

## Applying

- `--auto-safe` applies only safe updates. Risky ones go to `.cache/updates/pending.json` and show up in the TUI's
  Updates tab, where `enter` shows the upstream changelog, the risk verdict and the full diff.
- `--apply` applies what you selected; risky items still need `--allow-risky`.
- The directory swap is atomic, `SOURCE.json` gets the new commit, the index is rebuilt and the library repo gets a
  commit listing each skill and its `old → new` upstream commit.
- `rollback <name>` restores the previous version from the library's git history and pins it, so the next check
  does not offer the same update again. `lib-unpin` releases it.

## Projects

Library updates never change a project. Projects that hold an older copy are marked `outdated` in `status`, and
`update-projects` lists every registered project that has one. Refreshing is explicit: `update -C <dir>` in a project,
or `update-projects` for all of them, and both skip pinned and locally edited copies.

## Weekly timer (optional)

`scripts/systemd/skill-library-update.{service,timer}` run `update-library --auto-safe` once a week and send a desktop
notification. They are not installed by default:

```bash
cp scripts/systemd/skill-library-update.* ~/.config/systemd/user/
systemctl --user enable --now skill-library-update.timer
```
