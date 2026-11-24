# Using skillman in CI

Project skills are committed files that an agent follows, so they deserve the same gate as code. Two commands are
built for CI: neither needs your library or the network, only the `skillman` binary.

| Command | Fails when |
|---|---|
| `skillman verify .` | A committed skill differs from the hash in `.agents/skill-manifest.json`, a folder is not in the manifest, a manifest entry is missing on disk, or a `.claude/skills` link is broken. Pinned skills may differ |
| `skillman audit . --fail-on review` | A skill has validation errors or a risk signal: hooks, a download piped to a shell, credential reads, broad `allowed-tools`, symlinks. `--fail-on error` only fails on validation errors |

`verify` checks that the committed skill files and the committed manifest agree. It does not check where either
came from, so it catches an edit to a skill folder that forgot the manifest, not a deliberate change to both:

- A pull request that edits `SKILL.md` and also updates its `tree_hash` passes.
- A skill marked `"pinned": true` passes with any content.

To change a skill on purpose, update it from the library (`update`) or keep the edit and `pin` it, then commit the
manifest alongside it. Review those changes like code: require an owner review for `.agents/` and `.claude/skills/`,
for example with a CODEOWNERS entry:

```text
# .github/CODEOWNERS
/.agents/         @your-org/skill-owners
/.claude/skills/  @your-org/skill-owners
```

A diff that touches `.agents/skill-manifest.json` (a changed `tree_hash`, `upstream_commit` or `pinned`) is a change
to what the agent is told to do, even when `verify` passes.

## GitHub Actions

```yaml
# .github/workflows/skills.yml
name: skills
on: [pull_request]
permissions:
  contents: read
jobs:
  skills:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.26" }
      - name: Install skillman (pin to a commit you have reviewed)
        run: GOBIN="$RUNNER_TEMP/bin" go install github.com/hoangisreal/skillman/cmd/skillman@<commit-sha>
      - run: '"$RUNNER_TEMP/bin/skillman" verify .'
      - run: '"$RUNNER_TEMP/bin/skillman" audit . --fail-on review'
```

## pre-commit

```yaml
# .pre-commit-config.yaml
repos:
  - repo: local
    hooks:
      - id: skills-verify
        name: skills match manifest
        entry: skillman verify .
        language: system
        pass_filenames: false
        files: ^\.(agents|claude)/
```

## Output

```console
$ skillman verify .
✓ postgres: ok
✓ vitest: pinned, edited locally
✗ vue: content differs from the manifest hash (edited? pin it, or run update)
✗ stray-skill: untracked: on disk but not in the manifest (run: skillman adopt)

$ skillman audit . -q
! sketchy
    review: pipe download to shell in SKILL.md
12 skills: ok=10, info=1, review=1, error=0
```

`--json` on both commands prints machine-readable results (see [agents](agents.md#json-shapes)).
