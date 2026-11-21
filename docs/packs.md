# Packs

A pack is a named group of skills you install with one word: `skill-add @web-vue`.

```yaml
# ~/skill-library/packs/fullstack-vue.yaml
name: fullstack-vue
description: Vue frontend + Node backend + Postgres + Docker
extends: [web-vue, node-backend, postgres, docker]
skills: []
```

| Field | |
|---|---|
| `name` | kebab-case; must match the file name. `vendor/<owner>` for generated vendor packs |
| `description` | One line, shown by `skill-add --packs` and in the TUI |
| `extends` | Other packs to include. Cycles are rejected when the pack is saved |
| `skills` | Library skill names |

## Starter packs

The repository ships 34 stack packs in `packs/`, and `install.sh` copies any you do not have yet. A few examples:

| Pack | For |
|---|---|
| `@core` | Baseline workflow for any project: TDD, debugging, verification, PR review. Keep it small, it loads in every session |
| `@web-vue` · `@web-react` · `@web-svelte` · `@web-quality` · `@docs-site` | Frontend |
| `@mobile-rn` · `@flutter` · `@qt` | Mobile and desktop |
| `@python` · `@data-science` · `@ml-train` · `@ml-tracking` · `@ml-serve` · `@ml-project` · `@vision` · `@llm-apps` | Python, data and AI |
| `@node-backend` · `@jvm` · `@data-eng` | Backends and pipelines |
| `@postgres` · `@nosql` | Databases |
| `@docker` · `@iac` · `@k8s-gitops` · `@cloudflare` · `@ci-github` | Infrastructure |
| `@security` · `@research-writing` · `@office-docs` · `@skill-authoring` · `@web3` | Cross-cutting |
| `@fullstack-vue` · `@fullstack-react` | Composites built only from `extends` |

`skill-add --packs` prints them all with their sizes. Starter packs only list skills that the curated
`staging/repos.txt` can supply, so they resolve on a fresh library once you import.

## Vendor packs

`scripts/build-packs.py` writes `packs/vendor/<owner>.yaml` for every upstream owner with at least three skills, so
`@vendor/trailofbits` installs everything you imported from that owner. Vendor packs are regenerated on every run.
Stack packs are created only when they are missing, so your edits always win.

## Suggestions

`skill-add --suggest .` reads `package.json`, `pyproject.toml`, `requirements*.txt`, `go.mod`, `Cargo.toml`,
`pom.xml`, `build.gradle*`, `*.csproj`, `pubspec.yaml`, `Dockerfile`, `*.tf` and `.github/workflows`, then proposes the
packs that match, always starting with `@core`.

## Removing

`skillcore.py uninstall -C . @web-vue` removes the skills that came only from that pack. Skills that another installed
pack also uses, or that you added by name, stay. Removed copies go to the trash.

## Budget

Every installed skill's name and description is loaded into each agent session. `doctor` estimates that cost
(about `(name + description) / 4 + 8` tokens per skill, project and global skills together) and warns past
`SKILL_TOKEN_BUDGET` (default 2000). The Packs tab shows the cost of each pack before you install it.
