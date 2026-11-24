package core

import (
	"path/filepath"
	"strings"
)

type installDecision struct {
	action, reason, message string
	inLibrary, copyNeeded   bool
}

func (d installDecision) result(name string) Result {
	return res(name, d.action != "blocked", "%s", d.message)
}

func decideInstall(project, src string, item Item, prev *Entry, force, copies bool) installDecision {
	name := item.Name
	decision := installDecision{action: "blocked"}
	refuse := func(format string, args ...any) installDecision {
		decision.reason = sprintf(format, args...)
		decision.message = decision.reason
		return decision
	}
	if !KEBAB.MatchString(name) {
		return refuse("invalid skill name")
	}
	decision.inLibrary = IsFile(filepath.Join(src, "SKILL.md"))
	if !decision.inLibrary {
		if item.Pack != "" && !item.Manual {
			decision.action = "skipped"
			return refuse("skipped: not in the library yet (listed by @%s); %s", item.Pack, PackImportHint(item.Pack))
		}
		return refuse("not in library (try: skillman search %s)", strings.Split(name, "-")[0])
	}
	dest := Dest(project, name)
	if err := checkInstallTargets(project, name, prev, copies); err != nil {
		return refuse("%v", err)
	}
	if IsSymlink(dest) {
		return refuse("%s is a symlink; refusing", dest)
	}
	hash := inspectionHashes()
	switch {
	case !Exists(dest):
		decision.action = "copy"
		if prev != nil {
			decision.action = "restore"
		}
		decision.copyNeeded, decision.message = true, "copied"
		decision.reason = "copy skill files from the library"
	case prev == nil && !force:
		return refuse("untracked project copy: inspect with skillman diff, then use skillman adopt")
	case hash(src) == hash(dest):
		decision.action, decision.message = "already_present", "already up to date"
		decision.reason = "skill files already match the library"
		if !linkOK(project, name, copies, hash) {
			decision.action, decision.reason = "repair_link", "refresh the Claude link or copy"
		}
	case force:
		decision.action, decision.message = "replace", "replaced (--force)"
		decision.copyNeeded, decision.reason = true, "replace the project copy (--force)"
	case prev != nil && prev.TreeHash == hash(dest):
		return refuse("outdated (library is newer) — run: skillman update -C <dir> %s", name)
	default:
		return refuse("%s exists and differs from library (review with: skillman diff -C <dir> %s; or use --force)", dest, name)
	}
	if decision.copyNeeded && IsSymlink(filepath.Join(src, "SKILL.md")) {
		decision.action = "blocked"
		return refuse("%s: expected a regular SKILL.md", name)
	}
	if decision.copyNeeded {
		if err := checkSkillLinks(name, src); err != nil {
			decision.action = "blocked"
			return refuse("%v", err)
		}
	}
	if !decision.copyNeeded && !regularSkill(dest) {
		decision.action = "blocked"
		return refuse("%s: expected a regular SKILL.md", name)
	}
	return decision
}

type InstallPreviewItem struct {
	Name      string          `json:"name"`
	Pack      string          `json:"pack"`
	State     string          `json:"state"`
	InLibrary bool            `json:"in_library"`
	Tokens    int             `json:"tokens"`
	Action    string          `json:"action"`
	Reason    string          `json:"reason"`
	Targets   []InstallTarget `json:"targets,omitempty"`
}

type InstallTarget struct {
	Path   string   `json:"path"`
	Role   string   `json:"role"`
	Mode   string   `json:"mode"`
	Agents []string `json:"agents"`
}

func installTarget(name, root, role, mode string) InstallTarget {
	t := InstallTarget{Path: root + "/skills/" + name, Role: role, Mode: mode}
	for _, agent := range agentDirs {
		if Contains(agent.project, root) {
			t.Agents = append(t.Agents, agent.id)
		}
	}
	return t
}

type InstallPreview struct {
	Project     string               `json:"project"`
	Skills      []InstallPreviewItem `json:"skills"`
	OK          bool                 `json:"ok"`
	AddedTokens int                  `json:"added_tokens"`
	BudgetAfter int                  `json:"budget_after"`
	Budget      int                  `json:"budget"`
}

func PreviewInstall(project string, items []Item, force bool) (InstallPreview, error) {
	rows, man, err := InspectProject(project)
	if err != nil {
		return InstallPreview{}, err
	}
	states, budget := StatusMap(rows), TokenBudgetOf(project)
	preview := InstallPreview{Project: project, Skills: []InstallPreviewItem{}, BudgetAfter: budget.Total, Budget: budget.Budget}
	var results []Result
	copies := man.copyMode()
	for _, item := range items {
		name := item.Name
		decision := decideInstall(project, filepath.Join(SKILLS, name), item, man.Skills[name], force, copies)
		results = append(results, decision.result(name))
		row := InstallPreviewItem{
			Name: name, Pack: item.Pack, State: states[name].State,
			InLibrary: decision.inLibrary, Action: decision.action, Reason: decision.reason,
		}
		if row.InLibrary {
			row.Tokens = SkillTokens(filepath.Join(SKILLS, name))
		}
		switch row.Action {
		case "copy", "restore":
			preview.AddedTokens += row.Tokens
			preview.BudgetAfter += row.Tokens
		case "replace":
			preview.BudgetAfter += row.Tokens
			if regularSkill(Dest(project, name)) {
				preview.BudgetAfter -= SkillTokens(Dest(project, name))
			}
		}
		if row.Action != "blocked" && row.Action != "skipped" {
			mode := "symlink"
			if copies {
				mode = "copy"
			}
			row.Targets = []InstallTarget{
				installTarget(name, ".agents", "primary", "copy"),
				installTarget(name, ".claude", "mirror", mode),
			}
		}
		preview.Skills = append(preview.Skills, row)
	}
	preview.OK = SummarizeInstall(results).OK()
	return preview, nil
}
