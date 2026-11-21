# Security

## Threat model

A skill is text that an agent follows, plus files the agent may run, so a malicious or careless skill can make the
agent exfiltrate secrets, run installers or weaken its own guardrails. skillman's defences:

| Risk | Defence |
|---|---|
| A skill installs hooks or MCP servers | `hooks/`, `commands/`, subagents, `.mcp.json` and plugin manifests are never imported. Frontmatter `hooks:` is flagged |
| An update smuggles in new behaviour | Updates are compared with the current copy. New risk signals mark them risky, and risky updates are never applied automatically |
| Symlinks reach outside the repository | Links are inlined only if they resolve inside the upstream repo; others are dropped. `SKILL.md` itself may not be a link |
| Unlicensed code | A license is required at import, and an update that loses it is reported |
| Silent edits in a project | `verify` fails CI when a committed skill no longer matches its manifest hash |
| Path traversal through names | Skill and pack names must be kebab-case; anything else is refused |

The scanner is pattern-based. It catches the common cases (a download piped to a shell, encoded payloads, credential
reads, reverse shells, prompt-injection phrasing, broad `allowed-tools`), not a determined attacker. Read a skill
before you install it, especially its scripts.

## Reporting a vulnerability

Please report it privately through
[GitHub Security Advisories](https://github.com/hoangisreal/skillman/security/advisories/new) instead of opening a
public issue. Include the version (commit), steps to reproduce and the impact. You should get a reply within a week.
