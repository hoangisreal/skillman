package cli

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/hoangisreal/skillman/internal/core"
)

type importReport struct {
	core.ImportSource
	Project   string               `json:"project,omitempty"`
	Library   []core.LibraryImport `json:"library,omitempty"`
	Install   []core.Result        `json:"install,omitempty"`
	ApplyArgs []string             `json:"apply_args,omitempty"`
	RetryArgs []string             `json:"retry_args,omitempty"`
	Error     string               `json:"error,omitempty"`
}

func (r importReport) output(a *Args, err error) int {
	if err != nil {
		r.Error = err.Error()
	}
	if a.Has("json") {
		printJSON(r)
	} else {
		if r.Commit != "" {
			outf("%s @ %s\n", r.Repo, r.Commit)
		}
		if r.Project != "" {
			outf("Project %s\n", r.Project)
		}
		if len(r.Library) == 0 {
			for _, s := range r.Skills {
				outf("  %s (%s)  %s\n", s.Name, s.Path, s.Description)
			}
		}
		for _, s := range r.Library {
			outf("  library  %s  %s (%s)  %s\n", s.Action, s.Name, s.Path, s.Detail)
		}
		for _, s := range r.Install {
			outf("  project  %s  %s\n", s.Name, s.Message)
		}
		if len(r.RetryArgs) > 0 {
			outf("retry args: ")
			printJSON(r.RetryArgs)
		}
		if err != nil {
			errfln("%s", err)
		}
	}
	if err != nil {
		return 1
	}
	for _, s := range r.Library {
		if s.Action == "failed" {
			return 1
		}
	}
	for _, s := range r.Install {
		if !s.OK {
			return 1
		}
	}
	return 0
}

func cmdImport(a *Args) int {
	var r importReport
	if len(a.Pos) != 1 {
		return usage("import <owner/repo or GitHub URL> [--list | --skill name ...] [-C project] [--dry-run] [--json]")
	}
	for key := range a.bools {
		switch key {
		case "list", "dry-run", "allow-risky", "json":
		case "C", "skill", "commit":
			return usage("missing value for " + key)
		default:
			return usage("unknown import flag: " + key)
		}
	}
	for key, values := range a.vals {
		if key != "C" && key != "skill" && key != "commit" {
			return usage("unexpected import option: " + key)
		}
		for _, v := range values {
			if strings.TrimSpace(v) == "" || strings.HasPrefix(v, "--") {
				return usage("missing value for " + key)
			}
		}
	}
	if a.Has("skill") && len(a.List("skill")) == 0 {
		return usage("--skill requires a name")
	}
	src, err := core.DiscoverImport(a.Pos[0], a.Str("commit", ""))
	r.ImportSource = src
	if err != nil || a.Has("list") {
		return r.output(a, err)
	}
	selected, err := src.Select(a.List("skill"))
	if err != nil || len(selected) == 0 {
		return r.output(a, err)
	}
	r.Skills = selected
	// Keep directory scoping on replay, even if a branch is renamed or deleted.
	pinnedSource := src.Repo
	if src.Subpath != "." {
		u := url.URL{Scheme: "https", Host: "github.com", Path: "/" + src.Repo + "/tree/" + src.Commit + "/" + src.Subpath}
		pinnedSource = u.String()
	}
	apply := []string{"import", pinnedSource, "--commit", src.Commit}
	for _, s := range selected {
		apply = append(apply, "--skill", s.Name)
	}
	if a.Has("C") {
		target := a.Str("C", "")
		r.Project, err = core.ResolveProject(target, false)
		if err != nil {
			r.Project, _ = filepath.Abs(target)
			return r.output(a, err)
		}
		apply = append(apply, "-C", r.Project)
	}
	if a.Has("allow-risky") {
		apply = append(apply, "--allow-risky")
	}
	if a.Has("json") {
		apply = append(apply, "--json")
	}
	dry := a.Has("dry-run")
	r.Library, err = core.ImportToLibrary(src, selected, dry, a.Has("allow-risky"))
	if err != nil {
		return r.output(a, err)
	}
	var items []core.Item
	for _, s := range r.Library {
		if s.Action != "failed" {
			items = append(items, core.Item{Name: s.Name, Manual: true})
		}
	}
	if dry && len(items) == len(selected) {
		r.ApplyArgs = apply
	}
	if r.Project == "" || len(items) == 0 {
		return r.output(a, nil)
	}
	if dry {
		if err = core.CheckProjectDirs(r.Project); err != nil {
			r.ApplyArgs = nil
			return r.output(a, err)
		}
		st := core.StatusMap(core.ProjectStatus(r.Project))
		for _, it := range items {
			state := st[it.Name].State
			plan := core.Result{Name: it.Name, OK: true, Message: "would install"}
			link := core.LinkP(r.Project, it.Name)
			switch {
			case core.Lexists(link) && !core.IsSymlink(link):
				plan.OK, plan.Message = false, "project link path is occupied"
			case state == "missing":
				plan.Message = "would restore missing copy"
			case state == "ok":
				plan.Message = "already present"
			case state != "":
				plan.OK, plan.Message = false, "existing project copy is "+state
			}
			r.Install = append(r.Install, plan)
			if !plan.OK {
				r.ApplyArgs = nil
			}
		}
		return r.output(a, nil)
	}
	r.Install, err = core.Install(r.Project, items, false)
	retry := []string{"add", "-C", r.Project}
	if err != nil {
		r.Install = nil // A manifest write error rolls back all copies in this call.
		for _, it := range items {
			r.Install = append(r.Install, core.Result{Name: it.Name, OK: false, Message: err.Error()})
			retry = append(retry, it.Name)
		}
	} else {
		for _, s := range r.Install {
			if !s.OK {
				retry = append(retry, s.Name)
			}
		}
	}
	if len(retry) > 3 {
		r.RetryArgs = retry
	}
	if err != nil {
		err = fmt.Errorf("project installation: %w", err)
	}
	return r.output(a, err)
}
