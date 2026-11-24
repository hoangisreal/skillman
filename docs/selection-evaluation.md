# Project skill selection measurements


## Labelled task set

`internal/search/eval_test.go` (`TestRecommendationQuality`) runs 20 labelled cases against a 22-skill catalog with six packs and nine project fixtures (Vue + PostgreSQL, React/Next.js, FastAPI, Rust, Go, PHP, Ruby, Swift and an empty folder). Eighteen cases have one correct skill; two have none and expect an empty shortlist. The test fails when any metric drops below the recorded level.

| Code | Top-1 | Top-3 | Empty when nothing fits | `install_args` missing the answer |
|---|---:|---:|---:|---:|
| `60653ce` (before whole-library task ranking and language detection) | 10/18 | 10/18 | 2/2 | 0 |
| Current | 18/18 | 18/18 | 2/2 | 0 |

The eight earlier misses were tasks outside the detected stacks (Docker or Helm in a Vue project, Terraform or CI in an empty folder) and Rust, PHP, Ruby and Swift projects. The catalog has one specialist per task, so the set checks routing rather than fine ranking among similar skills, and it was written by the same author as the ranking changes. Add cases from real requests, especially misses, before tuning weights.

## Project status in copy mode

On Linux, `status -C <project> --json` compared 100 tracked skills against matching Claude copies and a matching
local library. Each tree had a `SKILL.md` and a roughly 3 KB reference. Both binaries reported 100 healthy rows.
Timing uses 50 fresh processes per binary after five warmups, with the order alternated and the OS file cache enabled.
A separate `strace` counted successful opens of the manifest and one representative canonical `SKILL.md`.

| Build | p50 | p95 | Manifest opens | Canonical `SKILL.md` opens per skill |
|---|---:|---:|---:|---:|
| `f8f2929` | 56.59 ms | 68.17 ms | 101 | 2 |
| Shared manifest and hashes | 20.36 ms | 26.90 ms | 1 | 1 |

## Recommendation CLI

Each cell shows the full JSON command at `d8d8e84` followed by the new `--brief --json` command. Both ran `recommend -C <project> --task "vue test"` against the same files. Every run ranked `vue-testing-best-practices` first.

| Catalog | Project | Installed skills | First call, ms | Warm p50, ms | Warm p95, ms | JSON bytes | Skill file opens |
|---|---|---:|---:|---:|---:|---:|---:|
| Original 723 | Small | 0 | 713.8 → 709.2 | 31.8 → 24.4 | 36.4 → 29.5 | 18,660 → 854 | 70 → 1 |
| Synthetic 723 | Small | 0 | 183.7 → 185.3 | 28.2 → 14.5 | 36.0 → 18.2 | 10,123 → 770 | 483 → 1 |
| Synthetic 723 | Small | 2 | 190.1 → 183.6 | 31.8 → 15.1 | 36.1 → 19.0 | 10,238 → 772 | 489 → 7 |
| Synthetic 723 | Monorepo | 0 | 196.6 → 179.1 | 30.6 → 17.6 | 34.8 → 20.7 | 10,164 → 779 | 483 → 1 |
| Synthetic 723 | Monorepo | 2 | 190.0 → 181.0 | 30.5 → 17.7 | 40.1 → 20.1 | 10,279 → 781 | 489 → 7 |
| Synthetic 3,000 | Small | 0 | 755.4 → 696.0 | 75.9 → 22.0 | 89.3 → 26.5 | 10,126 → 772 | 2,001 → 1 |
| Synthetic 3,000 | Small | 2 | 756.3 → 697.3 | 74.4 → 24.3 | 84.9 → 32.8 | 10,241 → 774 | 2,007 → 7 |
| Synthetic 3,000 | Monorepo | 0 | 760.2 → 707.9 | 77.9 → 26.4 | 88.2 → 33.7 | 10,167 → 781 | 2,001 → 1 |
| Synthetic 3,000 | Monorepo | 2 | 757.3 → 724.9 | 79.4 → 27.3 | 97.6 → 32.1 | 10,282 → 783 | 2,007 → 7 |

The original-catalog fixture copied the local index, packs and skill descriptions into a temporary library. The synthetic fixtures used the shipped packs, three named skills and a repeating sequence of Vue, PostgreSQL and unrelated guides. Small projects had one Vue and PostgreSQL package manifest. Monorepos had 40 under `packages/`. Populated projects used `add` to install `vue-best-practices` and `postgres-database-migration`.

Each sample launched a fresh binary. The first call started with an index and no classification cache. The OS file cache remained enabled. Warm timings use 50 invocations for the original catalog and 25 for each synthetic fixture, with nearest-rank p95. Bytes count stdout including whitespace. A separate warm `strace -f -e openat` run counted successful opens of `SKILL.md`, including project hashing and budget reads. On the original fixture, output shrank 95.4% and warm p95 fell 19.0%.

## Agent routing

### Task-first ranking check

The fixtures cover a longer specialist winning on task fit, the shorter of two equivalent guides winning
on added description cost, and a matching installed guide taking precedence over an equivalent uninstalled
guide. CLI and TUI tests also cover a missing indexed skill, empty projects, manual selection and a cancelled
preview.

On Linux, the same warm `recommend --task "vue test" --brief --json` request ranked
`vue-testing-best-practices` first in every run. Each row uses 25 runs per build, alternating their order
after one warmup per build. The baseline is `207c313`. File opens are from a separate warm `strace` run.

| Synthetic catalog | p50, ms | p95, ms | JSON bytes | Successful `SKILL.md` opens |
|---|---:|---:|---:|---:|
| 723 skills | 15.4 → 15.3 | 19.7 → 18.6 | 774 → 774 | 6 → 6 |
| 3,000 skills | 24.0 → 24.5 | 31.0 → 29.0 | 776 → 776 | 6 → 6 |

A live terminal run selected a Vue Router skill in place of the proposed Vue testing skill, cancelled
and reopened the preview, installed the manual selection and verified all seven tracked copies.

### Client runs with the task-first workflow

| Client | Local shortlist | Preview and install exactly one skill | Ordinary code edit |
|---|---|---|---|
| Codex | Called `recommend --brief --json` and left the fixture unchanged | Previewed, installed and verified the selected Vue testing skill | Updated `double.js` without calling Skillman |
| Claude Code | One `recommend --brief --json` call and no fixture changes | Called preview, add, status and verify, but the environment reported a read-only filesystem and no install appeared | Updated `double.js` and ran Node without calling Skillman |
| OpenCode | Timed out without reaching the fixture binary | Timed out without reaching the fixture binary | Timed out before editing |

### Earlier client runs

Each agent received a fresh Vue project containing both `skillman` and `find-skills`. A wrapper recorded arguments passed to the fixture binary. Project changes were checked independently of the agent's response.

| Case | Vietnamese prompt |
|---|---|
| Local | Từ thư viện Skillman local, đề xuất tối đa ba skill để kiểm thử component Vue trong project này. Chưa cài gì. |
| Upstream | Tìm skill upstream để kiểm thử Vue trong repo GitHub vuejs-ai/skills. Cho tôi biết tên và nguồn, chưa cài gì. |
| Coding | Sửa hàm double trong double.js để double(3) trả về 6. |

The table counts calls observed by the binary wrapper. `<project>` stands for the absolute fixture path.

| Agent | Case | Skillman calls | Arguments | Project files changed | Outcome |
|---|---|---:|---|---|---|
| Codex | Local | 1 | `recommend -C <project> --brief --json --task "vue test"` | None | Completed |
| Codex | Upstream | 1 | `import vuejs-ai/skills --list --json` | None | Completed |
| Codex | Coding | 0 | — | `double.js` | Completed and ran the Node assertion |
| Claude Code | Local | 1 | `recommend -C <project> --brief --json --task "vue test"` | None | Completed |
| Claude Code | Upstream | 1 | `import vuejs-ai/skills --list --json` | None | Completed |
| Claude Code | Coding | 0 | — | `double.js` | Changed the function to `return x * 2` and invoked Node. The final run timed out at 100 s after the edit. |
| OpenCode | Local | 0 | — | None | Timed out at 100 s during fixture discovery |
| OpenCode | Upstream | 0 | — | None | Attempted `--help` and web search. No call reached the fixture binary before the 100 s timeout. |
| OpenCode | Coding | 0 | — | None | Attempted to edit `double.js` and timed out at 100 s. The fixture diff was empty. |

Codex initially failed to initialize its app server with read-only runtime state. The completed runs used its regular runtime. An earlier Claude Code run completed the coding prompt, while the timed run above records the file change independently. OpenCode's three results leave its routing unverified.


## Additional local clients

Cursor's CLI executable was unavailable on the Linux evaluation machine. GitHub Copilot CLI 1.0.83 started and reported an authentication error for the Vietnamese local-selection prompt. The fixture wrapper recorded zero Skillman calls, and the requested Vue testing skill was absent after the run. Discovery and installation in both clients still need an authenticated live check.

## GitHub import

A separate live smoke test listed eight skills in `vuejs-ai/skills`. The preview selected `skills/vue-testing-best-practices` at `198f0cd8f7f9c0a3d9b566ccc16de6677519ee78`. Running its `apply_args` imported the skill into a temporary library, installed the requested project copy and passed `skillman verify`. The CLI integration test uses a local Git remote for branch movement, collisions, license and audit checks, and project-install failure and retry.
