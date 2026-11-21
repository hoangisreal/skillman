# Using skillman in CI

Project skills are committed files that an agent follows, so they deserve the same gate as code. Two commands are
built for CI: neither needs your library or the network, only `skillcore.py` and PyYAML.

| Command | Fails when |
|---|---|
| `skillcore.py verify .` | A committed skill differs from the hash in `.agents/skill-manifest.json`, a folder is not in the manifest, a manifest entry is missing on disk, or a `.claude/skills` link is broken. Pinned skills may differ |
| `skillcore.py audit . --fail-on review` | A skill has validation errors or a risk signal: hooks, a download piped to a shell, credential reads, broad `allowed-tools`, symlinks. `--fail-on error` only fails on validation errors |

`verify` works like a frozen lockfile: a pull request cannot quietly change what the agent is told to do. To change
a skill on purpose, update it from the library (`update`) or keep the edit and `pin` it, then commit the manifest
alongside it.

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
      - uses: actions/setup-python@v5
        with: { python-version: "3.12" }
      - name: Fetch skillcore (pin to a commit you have reviewed)
        run: |
          pip install pyyaml
          curl -fsSLo "$RUNNER_TEMP/skillcore.py" \
            https://raw.githubusercontent.com/hoangisreal/skillman/<commit-sha>/scripts/skillcore.py
      - run: python "$RUNNER_TEMP/skillcore.py" verify .
      - run: python "$RUNNER_TEMP/skillcore.py" audit . --fail-on review
```

## pre-commit

```yaml
# .pre-commit-config.yaml
repos:
  - repo: local
    hooks:
      - id: skills-verify
        name: skills match manifest
        entry: python3 ~/skill-library/scripts/skillcore.py verify .
        language: system
        pass_filenames: false
        files: ^\.(agents|claude)/
```

## Output

```console
$ skillcore.py verify .
✓ postgres: ok
✓ vitest: pinned, edited locally
✗ vue: content differs from the manifest hash (edited? pin it, or run update)
✗ stray-skill: untracked: on disk but not in the manifest (run: skillcore.py adopt)

$ skillcore.py audit . -q
! sketchy
    review: pipe download to shell in SKILL.md
12 skills: ok=10, info=1, review=1, error=0
```
