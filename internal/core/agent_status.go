package core

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

type AgentStatusReport struct {
	Project       string        `json:"project"`
	IncludeGlobal bool          `json:"include_global"`
	Agents        []AgentStatus `json:"agents"`
}

type AgentStatus struct {
	ID         string           `json:"id"`
	Roots      []AgentSkillRoot `json:"roots"`
	Skills     []AgentSkill     `json:"skills"`
	Duplicates []AgentDuplicate `json:"duplicates"`
}

type AgentSkillRoot struct {
	Path   string `json:"path"`
	Scope  string `json:"scope"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

type AgentSkill struct {
	Name    string   `json:"name"`
	Path    string   `json:"path"`
	Scope   string   `json:"scope"`
	Tracked bool     `json:"tracked"`
	State   string   `json:"state"`
	Reason  string   `json:"reason,omitempty"` // why the state is not "available"
	Aliases []string `json:"aliases,omitempty"`
}

type AgentDuplicate struct {
	Name  string   `json:"name"`
	Paths []string `json:"paths"`
}

type agentSkillDirs struct {
	id      string
	project []string
	global  []string
}

var agentDirs = []agentSkillDirs{
	{"claude-code", []string{".claude"}, []string{".claude"}},
	{"codex", []string{".agents"}, []string{".agents"}},
	{"cursor", []string{".agents", ".cursor", ".claude", ".codex"}, []string{".agents", ".cursor", ".claude", ".codex"}},
	{"github-copilot", []string{".agents", ".github", ".claude"}, []string{".agents", ".copilot"}},
	{"opencode", []string{".agents", ".opencode", ".claude"}, []string{".agents", ".config/opencode", ".claude"}},
}

type scannedSkill struct {
	path, resolved, state, reason string
}

type scannedSkillRoot struct {
	state, reason string
	skills        map[string]scannedSkill
}

func pathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// InspectAgents lists visible skill locations without invoking agents or reading their configuration.
func InspectAgents(project string, includeGlobal bool) (AgentStatusReport, error) {
	report := AgentStatusReport{Project: project, IncludeGlobal: includeGlobal}
	man, _, err := LoadProjectManifest(project)
	if err != nil {
		return report, err
	}
	var home string
	if includeGlobal {
		home, err = os.UserHomeDir()
		if err != nil {
			return report, err
		}
		if resolved, err := filepath.EvalSymlinks(home); err == nil {
			home = resolved
		}
	}
	type root struct{ path, scope string }
	pathRoots := make(map[string]bool)
	rootsByAgent := make(map[string][]root)
	for _, agent := range agentDirs {
		for _, dir := range agent.project {
			path := filepath.Join(project, dir, "skills")
			rootsByAgent[agent.id] = append(rootsByAgent[agent.id], root{path, "project"})
			pathRoots[pathKey(path)] = true
		}
		if includeGlobal {
			for _, dir := range agent.global {
				path := filepath.Join(home, dir, "skills")
				rootsByAgent[agent.id] = append(rootsByAgent[agent.id], root{path, "global"})
				pathRoots[pathKey(path)] = true
			}
		}
	}
	scans := map[string]scannedSkillRoot{}
	copies, hash := man.copyMode(), inspectionHashes()
	trackedNames := man.SkillNames()
	canonicalRoot := pathKey(Dest(project, ""))
	claudeRoot := pathKey(LinkP(project, ""))
	canonicalRootSafe := !IsSymlink(Dest(project, ""))
	for _, agent := range agentDirs {
		view := AgentStatus{ID: agent.id, Roots: []AgentSkillRoot{}, Skills: []AgentSkill{}, Duplicates: []AgentDuplicate{}}
		available := map[string][]int{}
		locations := map[[2]string]int{}
		for _, r := range rootsByAgent[agent.id] {
			rootPath := pathKey(r.path)
			scan, ok := scans[rootPath]
			if !ok {
				scan = scanSkillRoot(r.path, pathRoots)
				scans[rootPath] = scan
			}
			view.Roots = append(view.Roots, AgentSkillRoot{agentReportPath(project, r.path, r.scope), r.scope, scan.state, scan.reason})
			canonical := r.scope == "project" && rootPath == canonicalRoot
			claude := r.scope == "project" && rootPath == claudeRoot
			names := make(map[string]bool)
			for name := range scan.skills {
				names[name] = true
			}
			if canonical || claude {
				for _, name := range trackedNames {
					names[name] = true
				}
			}
			sorted := make([]string, 0, len(names))
			for name := range names {
				sorted = append(sorted, name)
			}
			slices.Sort(sorted)
			for _, name := range sorted {
				entry, found := scan.skills[name]
				tracked := (canonical || claude) && man.Skills[name] != nil
				if !found {
					entry = scannedSkill{path: filepath.Join(r.path, name), state: "missing"}
					if scan.state != "available" && scan.state != "missing" {
						entry.state, entry.reason = scan.state, scan.reason
					} else if scan.state == "available" && Lexists(entry.path) {
						entry.state = "invalid"
						if entry.reason = pathReason(entry.path); entry.reason == "" {
							entry.reason = "not a folder or a link to one"
						}
					}
				}
				row := AgentSkill{Name: name, Path: agentReportPath(project, entry.path, r.scope), Scope: r.scope, Tracked: tracked, State: entry.state, Reason: entry.reason}
				if tracked && claude && row.State == "available" {
					if canonicalRootSafe && linkOK(project, name, copies, hash) {
						entry.resolved = Dest(project, name)
					} else {
						row.State = "drifted"
						row.Reason = "differs from .agents/skills/" + name + " (run: skillman repair-links)"
					}
				}
				if entry.state == "available" {
					key := [2]string{name, pathKey(entry.resolved)}
					if i, ok := locations[key]; ok {
						if row.State == "available" && view.Skills[i].State == "available" {
							view.Skills[i].Tracked = view.Skills[i].Tracked || tracked
							if row.Path != view.Skills[i].Path && !slices.Contains(view.Skills[i].Aliases, row.Path) {
								view.Skills[i].Aliases = append(view.Skills[i].Aliases, row.Path)
							}
							continue
						}
					} else {
						locations[key] = len(view.Skills)
						available[name] = append(available[name], len(view.Skills))
					}
				}
				view.Skills = append(view.Skills, row)
			}
		}
		for name, indices := range available {
			if len(indices) > 1 {
				paths := make([]string, 0, len(indices))
				for _, i := range indices {
					paths = append(paths, view.Skills[i].Path)
				}
				view.Duplicates = append(view.Duplicates, AgentDuplicate{name, paths})
			}
		}
		slices.SortFunc(view.Duplicates, func(a, b AgentDuplicate) int { return strings.Compare(a.Name, b.Name) })
		report.Agents = append(report.Agents, view)
	}
	return report, nil
}

func agentReportPath(project, path, scope string) string {
	if scope == "project" {
		if rel, err := filepath.Rel(project, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func scanSkillRoot(path string, roots map[string]bool) scannedSkillRoot {
	resolved, err := filepath.EvalSymlinks(path)
	switch {
	case os.IsNotExist(err):
		return scannedSkillRoot{state: "missing"}
	case err != nil:
		return scannedSkillRoot{state: "invalid", reason: ExplainFSError(err).Error()}
	case pathKey(path) != pathKey(resolved):
		return scannedSkillRoot{state: "external_link", reason: "links outside the project to " + resolved}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		reason := pathReason(path)
		if reason == "" {
			reason = ExplainFSError(err).Error()
		}
		return scannedSkillRoot{state: "invalid", reason: reason}
	}
	scan := scannedSkillRoot{state: "available", skills: map[string]scannedSkill{}}
	for _, child := range entries {
		if strings.HasPrefix(child.Name(), ".") || !(child.IsDir() || child.Type()&os.ModeSymlink != 0) {
			continue
		}
		entry := scanAgentSkill(filepath.Join(path, child.Name()), child.Name(), roots)
		scan.skills[child.Name()] = entry
	}
	return scan
}

func scanAgentSkill(path, name string, roots map[string]bool) scannedSkill {
	entry := scannedSkill{path: path, resolved: path, state: "invalid"}
	if target, err := os.Readlink(path); err == nil {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		if !roots[pathKey(filepath.Dir(target))] {
			entry.state, entry.reason = "external_link", "links outside the skill folders to "+target
			return entry
		}
		real, err := filepath.EvalSymlinks(target)
		if err != nil {
			entry.reason = "broken link to " + target
			return entry
		}
		if !roots[pathKey(filepath.Dir(real))] {
			entry.state, entry.reason = "external_link", "links outside the skill folders to "+real
			return entry
		}
		entry.resolved = real
	}
	if !regularSkill(entry.resolved) {
		if entry.reason = pathReason(filepath.Join(entry.resolved, "SKILL.md")); entry.reason == "" {
			entry.reason = "no regular SKILL.md file"
		}
		return entry
	}
	meta := SkillMeta(entry.resolved)
	desc, _ := meta["description"].(string)
	actualName, _ := meta["name"].(string)
	switch {
	case !KEBAB.MatchString(name):
		entry.reason = "folder name is not lowercase kebab-case"
	case actualName != name:
		entry.reason = sprintf("SKILL.md name is %q, folder is %q", actualName, name)
	case strings.TrimSpace(desc) == "":
		entry.reason = "SKILL.md has no description"
	default:
		entry.state = "available"
	}
	return entry
}
