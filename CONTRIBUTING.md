# Contributing

Issues and pull requests are welcome. Please keep changes small and focused.

## Setup

```bash
git clone https://github.com/hoangisreal/skillman
cd skillman
SKILL_LIBRARY="$PWD/.dev-lib" BIN_DIR="$PWD/.dev-lib/bin" ./install.sh --no-global-skill
```

Go 1.26 or newer and git are the only requirements.

## Tests

```bash
gofmt -l . && go vet ./... && go test -race ./...
```

The tests are fully offline. Each test points `SKILL_LIBRARY`, `SKILLMAN_STATE`, `SKILLCORE_GIT_BASE` and `HOME` at a
temp dir and calls `core.Configure()`, builds a fake upstream as a bare git repo served over `file://` with
partial-clone support, and imports from it. They cover:

- golden outputs of the Python version (`internal/core/testdata/compat`): content hashes, validation, tokens, pack
  YAML and diffs must stay byte-identical;
- pack expansion and cycles; install, update, conflict, adopt and pin flows; trash round-trips;
- refusal of path traversal and symlinks;
- tree-OID change detection, safe and risky updates, license removal, symlink inlining, rollback;
- taxonomy classification, overrides and `recommend`;
- the CLI as an agent uses it (`internal/cli`), and the TUI driven key by key and click by click (`internal/tui`).

Optional checks against a real library (not run in CI):

```bash
SKILLMAN_BENCH_LIBRARY=~/skill-library go test ./internal/tui -run LatencyReport -v     # per-key latency
mkdir -p /tmp/skillman-model-screens
SKILLMAN_SCREENSHOTS=/tmp/skillman-model-screens SKILLMAN_BENCH_LIBRARY="$PWD/.dev-lib" go test ./internal/tui -run Screenshots
```

The screenshot test generates SVG model snapshots against the scratch library from Setup. For the README gallery,
capture the running binary in a terminal using a demo project and save the frames as PNGs in `docs/screenshots`.

## Guidelines

- Logic goes in `internal/core`. The CLI and the TUI only call it; every TUI action must also be a CLI command.
- Every write to user data needs a test. Anything that removes data must move it to the trash.
- On-disk formats are shared with existing libraries and projects: keep them compatible (and the golden tests green).
- New risk signals go in `Suspicious` or `Assess()`, with a test that shows them being caught.
- Nothing slow on the TUI's Update path: use `m.async` for disk-heavy or network work.
- Keep dependencies minimal (Bubble Tea, Lip Gloss, x/ansi, go-yaml, go-difflib).

## Adding upstream repositories

Add a line to `staging/repos.txt` (`tier group owner/repo [subpath-hint]`). Only repositories with a license are
imported. Large community collections are not bulk-imported: list them so that people can cherry-pick from them.
