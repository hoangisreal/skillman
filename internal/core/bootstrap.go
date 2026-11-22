package core

import (
	"os"
	"path/filepath"
	"strings"
)

type StarterSource struct {
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Subpath string `json:"subpath"`
	Tier    string `json:"tier"`
}

// Pins make the offline preview and the subsequent import describe the same tree.
func starterSources() map[string]StarterSource {
	sources := map[string]StarterSource{}
	group := func(repo, commit, tier, dir, names string) {
		for _, name := range strings.Fields(names) {
			sources[name] = StarterSource{repo, commit, dir + "/" + name, tier}
		}
	}
	group("mattpocock/skills", "abeeaf95660ba9e8b7c753eb38f441ca707a8823", "community", "skills/engineering", "tdd diagnosing-bugs code-review")
	sources["review-pr"] = sources["code-review"]
	group("obra/superpowers", "8ca22dba9a94f28898bbce59f2537ff4d87c747d", "community", "skills", "verification-before-completion")
	group("antfu/skills", "4a6bb39e7b45e26402eb215393c51ccf5df1ca4c", "official", "skills", "vue vite vitest pinia nuxt nitro unocss")
	group("vuejs-ai/skills", "198f0cd8f7f9c0a3d9b566ccc16de6677519ee78", "official", "skills", "create-adaptable-composable vue-best-practices vue-router-best-practices vue-pinia-best-practices vue-testing-best-practices vue-debug-guides")
	group("neondatabase/postgres-skills", "27fe45e0f71ea89a6eaf9ea4d2e4068957c81c26", "official", "skills", "postgres-best-practices")
	group("timescale/pg-aiguide", "1aef6e452b97fae580d57677a807fd0d601ddc7b", "official", "skills", "postgres design-postgres-tables postgres-database-migration postgres-hybrid-text-search pgvector-semantic-search schema-exploration")
	return sources
}

type BootstrapItem struct {
	Name   string         `json:"name"`
	Source *StarterSource `json:"source,omitempty"`
	Status string         `json:"status"`
	Detail string         `json:"detail,omitempty"`
}

func Bootstrap(pack string, dry, allowRisky bool) ([]BootstrapItem, error) {
	switch pack {
	case "core", "web-vue", "postgres":
	default:
		return nil, errf("supported starter packs: core, web-vue, postgres")
	}
	names, err := ExpandPack(pack)
	if err != nil {
		return nil, err
	}
	return bootstrapSkills(names, starterSources(), dry, allowRisky)
}

func bootstrapSkills(names []string, sources map[string]StarterSource, dry, allowRisky bool) ([]BootstrapItem, error) {
	if !dry {
		unlock, err := DirLock(SKILLS)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	items := make([]BootstrapItem, 0, len(names))
	for _, name := range names {
		item := BootstrapItem{Name: name}
		source, known := sources[name]
		if known {
			item.Source = &source
		}
		switch {
		case !KEBAB.MatchString(name):
			item.Status = "failed"
			item.Detail = "invalid skill name"
		case IsFile(filepath.Join(SKILLS, name, "SKILL.md")):
			item.Status = "already_present"
		case Lexists(filepath.Join(SKILLS, name)):
			item.Status = "failed"
			item.Detail = "library path already exists"
		case !known:
			item.Status = "unavailable"
			item.Detail = "no pinned starter source"
		case dry:
			item.Status = "planned"
		default:
			item.Status = "imported"
			if note, err := importStarter(name, source, allowRisky); err != nil {
				item.Status = "failed"
				item.Detail = err.Error()
			} else {
				item.Detail = note
			}
		}
		items = append(items, item)
	}
	if !dry {
		_, _, _, err := RebuildIndex(true)
		return items, err
	}
	return items, nil
}

func importStarter(name string, source StarterSource, allowRisky bool) (string, error) {
	staged, _, err := Materialize(source.Repo, source.Commit, source.Subpath, name, source.Tier, nil)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staged)
	report := auditSkill(staged, name)
	note := ""
	switch {
	case report.Level == "error":
		return "", errf("audit %s: %s", report.Level, strings.Join(report.Errors, ", "))
	case report.Level == "review" && !allowRisky:
		return "", errf("audit %s: %s (re-run with --allow-risky to import anyway)", report.Level, strings.Join(report.Review, ", "))
	case report.Level == "review":
		note = "imported despite audit review: " + strings.Join(report.Review, ", ")
	}
	if err := os.Rename(staged, filepath.Join(SKILLS, name)); err != nil {
		return "", err
	}
	return note, nil
}
