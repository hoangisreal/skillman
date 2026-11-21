// Package skillman ships the starter data that `skillman init` copies into a new skill library:
// stack packs, the curated upstream repo list and the skill that teaches agents to use skillman.
package skillman

import "embed"

//go:embed packs/*.yaml staging/repos.txt skills/skillman/SKILL.md
var Assets embed.FS
