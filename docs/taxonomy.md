# Browse: domains, stacks and taxonomy.yaml

The Browse tab, `skillman browse`, `search stack:… domain:…` and `recommend` file every library skill under a
**domain** (Web frontend, Backend & APIs, Databases, ML, LLM apps, Cloud & DevOps, Security, …) and one or more
**stacks** inside it (Vue / Nuxt, PostgreSQL, Docker, MCP servers, …). The built-in taxonomy has 16 domains and about
70 stacks; `skillman taxonomy` prints it.

## How a skill is classified

For each stack, in this order of strength:

1. **Pack membership**: the skill is listed in one of the stack's `packs` (e.g. `@postgres` → PostgreSQL).
2. **Name**: the stack's `name` regex matches the skill name, or its `match` regex matches the name.
3. **Description**: the stack's `match` regex matches the description (weak; ignored when a stronger match exists).

The strongest stack gives the skill its primary domain; up to four stacks are kept. A skill with no stack falls back
to its legacy tag's domain, else **General / other**. Results are cached in `.cache/classify.json`; editing a pack or
`taxonomy.yaml` invalidates the cache automatically.

Your project's stacks come from the same detection as `suggest` (`package.json`, `pyproject.toml`, `Dockerfile`, …;
`go.mod`, `Cargo.toml`, `composer.json`, `Gemfile` and `Package.swift` mark the Go, Rust, PHP, Ruby and Swift stacks):
each suggested pack maps to its stacks, which are marked ★ and listed under **This project**.

## Customizing

Create `~/skill-library/taxonomy.yaml` (start from `skillman taxonomy --default` and keep only what you change):

```yaml
skills:                       # re-file individual skills (always wins)
  my-deploy-flow: {domain: cloud-devops, stacks: [ci-cd]}
  pyroscope: {stacks: [observability]}

domains:                      # an existing id replaces that domain, a new id is appended
  - id: game-dev
    title: Game development
    stacks:
      - id: godot
        title: Godot
        packs: [godot]                      # your pack
        match: '\b(godot|gdscript)\b'
```

Stack fields: `id` (unique across domains), `title`, `packs`, `name` (regex on the kebab-case name), `match` (regex
on "name with spaces + description", lower-cased), `not` (regex that vetoes `name`/`match`). Regexes use Go (RE2)
syntax. Invalid regexes are reported in the TUI status line and by `skillman taxonomy`.

The classification is also written to `INDEX.json` (`domain`, `stacks`) for tools that read the index.
