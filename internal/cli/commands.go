package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hoangisreal/skillman"
	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

var searchValues = []string{"domain", "stack", "limit", "C"}

func init() {
	// discovery
	reg("search", "<words…> [--domain D] [--stack S] [--limit N] [-C dir] [--json]",
		"Search library skills (ranked: name, then description).\n"+
			"Query syntax: words · -word (exclude) · tag:web · src:redis · pack:core · domain:backend · stack:vue ·\n"+
			"nopack · installed · notinstalled (states are read from -C dir, default .).", searchValues, cmdSearch)
	reg("info", "<skill> [--body] [-C dir] [--json]",
		"Show one skill: description, source, domain/stacks, packs, token cost, project state; --body adds SKILL.md.",
		[]string{"C"}, cmdInfo)
	reg("browse", "[--domain D] [--stack S] [-C dir] [--json]",
		"Skills grouped by domain › stack (the TUI Browse tab). Stacks used by the project are marked ★.",
		[]string{"domain", "stack", "C"}, cmdBrowse)
	reg("recommend", "[dir] [--task words] [--limit N] [--extra N] [--brief] [--json]",
		"Recommend skills for a project: detected packs with their skills, plus up to N (default 5, -1 = all)\n"+
			"other library skills of the same stacks. Shows what is installed and the token cost.",
		[]string{"task", "limit", "extra", "C"}, cmdRecommend)
	reg("suggest", "[dir] [--json]", "Suggest packs from the project's manifests (package.json, pyproject.toml, go.mod, …).",
		[]string{"C"}, cmdSuggest)
	reg("taxonomy", "[--default] [--json]",
		"Print the domain/stack taxonomy. --default prints the built-in YAML to start $SKILL_LIBRARY/taxonomy.yaml.",
		nil, cmdTaxonomy)

	// project
	reg("add", "<skill|@pack>… [dir] [-C dir] [--force] [--dry-run] [--json]",
		"Copy skills and packs into a project (.agents/skills + .claude/skills link + manifest).", []string{"C"}, cmdAdd)
	reg("remove", "<skill|@pack>… [-C dir] [--json]",
		"Remove skills or a pack from a project (to trash; skills still used by another pack stay).", []string{"C"}, cmdRemove)
	reg("status", "[dir] [--json]", "State of every project skill: ok outdated modified conflict untracked missing orphan invalid.",
		[]string{"C"}, cmdStatus)
	reg("doctor", "[dir] [--json]", "Check links, drift, shadowing by global skills and the description token budget.",
		[]string{"C"}, cmdDoctor)
	reg("verify", "[dir] [--json]", "CI check: committed skills match the manifest hashes (needs no library).",
		[]string{"C"}, cmdVerify)
	reg("audit", "[dir|skill-dir] [--library] [--fail-on review|error|never] [-q] [--json]",
		"Offline risk scan: hooks, pipe-to-shell, credential reads, broad allowed-tools, symlinks, executables.",
		[]string{"fail-on"}, cmdAudit)
	reg("diff", "<skill> [-C dir]", "Unified diff library → project copy.", []string{"C"}, cmdDiff)
	reg("update", "[skills…] [-C dir] [--force] [--json]",
		"Refresh outdated project copies from the library (--force also replaces modified ones).", []string{"C"}, cmdUpdate)
	reg("update-projects", "[--dry-run] [--json]", "Refresh outdated skills in every registered project.", nil, cmdUpdateProjects)
	reg("adopt", "[skills…] [-C dir] [--json]", "Track skills copied into .agents/skills before the manifest existed.",
		[]string{"C"}, cmdAdopt)
	reg("pin", "<skills…> [-C dir]", "Keep a project copy as it is (skipped by update, accepted by verify).",
		[]string{"C"}, func(a *Args) int { return cmdPin(a, true) })
	reg("unpin", "<skills…> [-C dir]", "Undo pin.", []string{"C"}, func(a *Args) int { return cmdPin(a, false) })
	reg("repair-links", "[dir]", "Recreate missing or broken .claude/skills links.", []string{"C"}, cmdRepairLinks)
	reg("projects", "[--json]", "Projects registered by skillman (where skills were installed).", nil, cmdProjects)

	// packs
	reg("pack", "list|show|create|delete|rename|describe|extends|add|rm …",
		"Manage packs:\n"+
			"  pack list [--vendor] [-v] [--json]          pack show <pack> [--json]\n"+
			"  pack create <pack> [skills…] [-d DESC] [--extends a,b]\n"+
			"  pack add <pack> <skills…>                   pack rm <pack> <skills…>\n"+
			"  pack describe <pack> <description…>         pack extends <pack> [packs…]\n"+
			"  pack rename <old> <new>                     pack delete <pack>   (to trash)",
		[]string{"d", "description", "extends"}, cmdPack)

	// library
	reg("import", "<owner/repo|GitHub URL> [--list] [--skill name ...] [--commit SHA] [-C project] [--dry-run] [--allow-risky] [--json]",
		"Discover and import GitHub skills into the library. Supply -C to also install the selected skills into a project.",
		[]string{"skill", "commit", "C"}, cmdImport)
	reg("library", "check|apply|auto-safe|pending|rollback|pin|unpin|rebuild-index|validate|new|build-packs …",
		"Manage the library and upstream updates:\n"+
			"  library check [skills…] [--pack P] [--log] [--json]     read-only upstream check\n"+
			"  library apply [skills…] [--allow-risky] [--no-commit]   apply changed skills (asks nothing: be sure)\n"+
			"  library auto-safe                                       apply safe updates, queue risky ones\n"+
			"  library pending [--json]                                risky updates waiting for review\n"+
			"  library rollback <skill>                                restore the previous version and pin it\n"+
			"  library pin|unpin <skills…>                             freeze a skill against upstream updates\n"+
			"  library rebuild-index · library validate [--strict] [-v] · library build-packs\n"+
			"  library new <skill> [-d DESC]                           scaffold a local skill",
		[]string{"pack", "d", "description"}, cmdLibrary)
	reg("upstream", "verify|candidates|tick|import …",
		"Import pipeline:\n"+
			"  upstream verify [repos.txt] [-j 8]       sparse-clone upstream repos, list candidate skills\n"+
			"  upstream candidates [words] [--ticked] [--new] [--json]\n"+
			"  upstream tick [--tier T] [--max-per-repo N] [--repo R]… [--pack P]… [--reset] [--line N]… [--untick]\n"+
			"  upstream import [--dry-run] [--update] [--rename-collisions]",
		[]string{"j", "tier", "max-per-repo", "repo", "pack", "line"}, cmdUpstream)
	reg("trash", "list|restore <id>|empty --yes [ids…] [--json]", "Removed project copies and deleted packs.", nil, cmdTrash)

	// setup
	reg("init", "", "Create or refresh the library at $SKILL_LIBRARY: starter packs, repos.txt, the skillman skill, git.",
		nil, cmdInit)
	reg("bootstrap", "--pack <core|web-vue|postgres> [--dry-run] [--allow-risky] [--json]", "Preview or import missing starter skills into the library from pinned sources.", []string{"pack"}, cmdBootstrap)
	reg("agent-setup", "[--dry-run] [--remove] [--no-claude] [--no-codex]",
		"Link the skillman skill into ~/.claude/skills (Claude Code, OpenCode) and ~/.agents/skills (Codex),\n"+
			"so coding agents can use skillman in any project. Never overwrites a real directory.", nil, cmdAgentSetup)
	reg("tui", "[dir]", "Open the terminal UI (same as running skillman without a command).", nil, func(a *Args) int {
		d := ""
		if len(a.Pos) > 0 {
			d = a.Pos[0]
		}
		return runTUI(d)
	})

	// skillcore.py-compatible aliases
	alias("install", "add")
	alias("uninstall", "remove")
	alias("packs", "pack", "list")
	alias("new", "library", "new")
	alias("rebuild-index", "library", "rebuild-index")
	alias("rollback", "library", "rollback")
	alias("lib-pin", "library", "pin")
	alias("lib-unpin", "library", "unpin")
	commands = append(commands,
		command{name: "expand", hidden: true, run: cmdExpand},
		command{name: "validate-packs", hidden: true, run: cmdValidatePacks},
		command{name: "update-library", hidden: true, values: []string{"pack"}, run: cmdUpdateLibraryCompat},
	)
}

// ───────────── discovery ─────────────

func loadIndex() ([]core.IndexRow, error) {
	return core.NewRecommendationCatalog().Index()
}

func stateMap(dir string) map[string]string {
	out := map[string]string{}
	if dir == "" {
		return out
	}
	for _, s := range core.ProjectStatus(dir) {
		out[s.Name] = s.State
	}
	return out
}

func buildIndex(dir string) (*search.Index, error) {
	rows, err := loadIndex()
	if err != nil {
		return nil, err
	}
	return search.Build(rows, core.PacksBySkill(), stateMap(dir), core.LoadTaxonomy()), nil
}

func cmdSearch(a *Args) int {
	if len(a.Pos) == 0 && !a.Has("domain") && !a.Has("stack") {
		return usage("skillman search <words…> [--domain D] [--stack S] [--json]")
	}
	dir := ""
	if a.Has("C") || strings.Contains(strings.Join(a.Pos, " "), "installed") {
		d, err := core.ResolveProject(a.Str("C", "."), false)
		if err != nil {
			return fail(err)
		}
		dir = d
	}
	ix, err := buildIndex(dir)
	if err != nil {
		return fail(err)
	}
	qs := strings.Join(a.Pos, " ")
	if v := a.Str("domain", ""); v != "" {
		qs += " domain:" + v
	}
	if v := a.Str("stack", ""); v != "" {
		qs += " stack:" + v
	}
	hits := ix.Search(search.Parse(qs), nil)
	if n := a.Int("limit", 0); n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	if a.Has("json") {
		type hit struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Source      string   `json:"source"`
			Domain      string   `json:"domain"`
			Stacks      []string `json:"stacks"`
			Packs       []string `json:"packs"`
			State       string   `json:"state,omitempty"`
		}
		out := []hit{}
		for _, i := range hits {
			d := ix.Docs[i]
			out = append(out, hit{d.Row.Name, d.Row.Description, d.Row.Source, d.Row.Domain, nzs(d.Row.Stacks), nzs(d.StackPacks), d.State})
		}
		printJSON(map[string]any{"query": qs, "count": len(out), "skills": out})
		return 0
	}
	if len(hits) == 0 {
		errfln("no matches")
		return 1
	}
	legacy := a.Has("legacy")
	for _, i := range hits {
		d := ix.Docs[i]
		desc := d.Row.Description
		if len([]rune(desc)) > 110 {
			desc = string([]rune(desc)[:107]) + "..."
		}
		if legacy {
			outf("%-34s [%s] %s\n", d.Row.Name, strings.Join(d.Row.Tags, ","), desc)
			continue
		}
		cls := d.Row.Domain
		if len(d.Row.Stacks) > 0 {
			cls += "/" + strings.Join(d.Row.Stacks, ",")
		}
		st := ""
		if d.State != "" {
			st = " (" + d.State + ")"
		}
		outf("%-34s [%s]%s %s\n", d.Row.Name, cls, st, desc)
	}
	return 0
}

func nzs(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func cmdInfo(a *Args) int {
	if len(a.Pos) != 1 {
		return usage("skillman info <skill> [--body] [--json]")
	}
	name := a.Pos[0]
	if err := core.CheckName(name); err != nil {
		return fail(err)
	}
	d := filepath.Join(core.SKILLS, name)
	if !core.IsFile(filepath.Join(d, "SKILL.md")) {
		return fail(fmt.Errorf("%s is not in the library (try: skillman search %s)", name, strings.Split(name, "-")[0]))
	}
	errs, warns, row := core.CheckSkill(d, name)
	if row == nil {
		row = &core.IndexRow{Name: name}
	}
	pbs := core.PacksBySkill()
	core.LoadTaxonomy().Classify(row, pbs[name])
	src := core.LoadMap(filepath.Join(d, "SOURCE.json"))
	proj, _ := core.ResolveProject(a.Str("C", "."), false)
	state := stateMap(proj)[name]
	info := map[string]any{
		"name": name, "description": row.Description, "source": row.Source, "commit": src["commit"],
		"domain": row.Domain, "stacks": nzs(row.Stacks), "tags": row.Tags, "packs": nzs(pbs[name]),
		"tokens": core.SkillTokens(d), "lines": row.Lines, "has_scripts": row.HasScripts, "path": d,
		"errors": nzs(errs), "warnings": nzs(warns), "project": proj, "state": state,
		"install": "skillman add " + name,
	}
	body := ""
	if a.Has("body") {
		body, _ = core.ReadText(filepath.Join(d, "SKILL.md"))
		info["body"] = body
	}
	if a.Has("json") {
		printJSON(info)
		return 0
	}
	outf("%s  (%s)\n%s\n\n", name, row.Source, row.Description)
	outf("domain: %s · stacks: %s\npacks: %s\ntokens ≈ %d · %d lines · scripts: %v\n", row.Domain,
		strings.Join(row.Stacks, ", "), strings.Join(pbs[name], ", "), core.SkillTokens(d), row.Lines, row.HasScripts)
	if state != "" {
		outf("in project %s: %s\n", proj, state)
	} else {
		outf("install: skillman add %s\n", name)
	}
	for _, e := range errs {
		outf("ERROR %s\n", e)
	}
	for _, w := range warns {
		outf("WARN  %s\n", w)
	}
	if body != "" {
		outf("\n%s", body)
	}
	return 0
}

func cmdBrowse(a *Args) int {
	rows, err := loadIndex()
	if err != nil {
		return fail(err)
	}
	tax := core.LoadTaxonomy()
	proj, _ := core.ResolveProject(a.Str("C", "."), false)
	det := core.DetectedStacks(core.ProjectSignals(proj), tax)
	tree := core.BuildTree(rows, tax, det, core.PacksBySkill())
	dom, stk := a.Str("domain", ""), a.Str("stack", "")
	var sel []core.DomainNode
	for _, d := range tree {
		if dom != "" && !strings.HasPrefix(d.ID, dom) {
			continue
		}
		if stk != "" {
			var ss []core.StackNode
			for _, s := range d.Stacks {
				if s.ID == stk || strings.HasPrefix(s.ID, stk) {
					ss = append(ss, s)
				}
			}
			if len(ss) == 0 {
				continue
			}
			d.Stacks = ss
		}
		sel = append(sel, d)
	}
	if a.Has("json") {
		if sel == nil {
			sel = []core.DomainNode{}
		}
		printJSON(map[string]any{"project": proj, "domains": sel})
		return 0
	}
	if len(sel) == 0 {
		errfln("no matching domain/stack (see: skillman browse)")
		return 1
	}
	detail := dom != "" || stk != ""
	for _, d := range sel {
		star := " "
		if d.Detected {
			star = "★"
		}
		outf("%s %-34s %4d  %s\n", star, d.Title, len(d.Skills), d.ID)
		for _, s := range d.Stacks {
			if len(s.Skills) == 0 && !detail {
				continue
			}
			star := " "
			if s.Detected {
				star = "★"
			}
			outf("   %s %-31s %4d  %s\n", star, s.Title, len(s.Skills), s.ID)
			if detail {
				outf("       %s\n", strings.Join(s.Skills, " "))
			}
		}
		if len(d.Other) > 0 {
			outf("     %-31s %4d\n", "(other)", len(d.Other))
			if detail {
				outf("       %s\n", strings.Join(d.Other, " "))
			}
		}
	}
	if !detail {
		outf("\ndetails: skillman browse --stack <id> · skillman search stack:<id> · ★ = used by %s\n", proj)
	}
	return 0
}

func cmdRecommend(a *Args) int {
	d, err := projectDir(a, true)
	if err != nil {
		return fail(err)
	}
	catalog := core.NewRecommendationCatalog()
	rows, err := catalog.Index()
	if err != nil {
		return fail(err)
	}
	limit, err := strconv.Atoi(a.Str("limit", "5"))
	if err != nil || limit < 1 || (a.Has("limit") && a.Str("limit", "") == "") {
		return usage("--limit requires a positive integer")
	}
	brief := a.Has("brief")
	rec := search.Rank(catalog, d, rows, a.Str("task", ""), limit, a.Int("extra", 5), brief)
	if a.Has("json") {
		if brief {
			out := map[string]any{"project": rec.Project, "budget": rec.Budget, "shortlist": rec.Shortlist, "install_args": rec.InstallArgs}
			if rec.Message != "" {
				out["message"] = rec.Message
			}
			printJSON(out)
		} else {
			printJSON(rec)
		}
		return 0
	}
	outf("project: %s\n", d)
	if rec.Message != "" {
		outf("%s\n", rec.Message)
	}
	for _, c := range rec.Shortlist {
		outf("  %-32s +%dt · %s (%s) %s\n", c.Name, c.AddedTokens, c.Reason, c.Evidence, c.State)
	}
	if brief {
		return 0
	}
	if len(rec.Groups) == 0 {
		outf("no stack detected (try: skillman browse, skillman search <words>)\n")
		return 0
	}
	for _, g := range rec.Groups {
		mark := ""
		if g.Installed {
			mark = " [installed]"
		}
		label := g.Pack
		if label != "" {
			label = "@" + label
		} else {
			label = strings.Join(g.Stacks, ", ")
		}
		outf("\n%s%s — %s (%s)\n", label, mark, g.Description, g.Reason)
		for _, s := range g.Skills {
			tag := "  "
			if !s.InPack {
				tag = "+ "
			}
			st := ""
			if s.State != "" {
				st = " [" + s.State + "]"
			}
			desc := s.Description
			if len([]rune(desc)) > 80 {
				desc = string([]rune(desc)[:77]) + "..."
			}
			outf("  %s%-34s %3dt%s %s\n", tag, s.Name, s.Tokens, st, desc)
		}
	}
	outf("\n(+ = same stack, not in the pack) · description budget ≈ %d/%d tokens\n", rec.Budget.Total, rec.Budget.Budget)
	if rec.InstallHint != "" {
		outf("install: %s\n", rec.InstallHint)
	}
	return 0
}

func cmdSuggest(a *Args) int {
	d, err := projectDir(a, true)
	if err != nil {
		return fail(err)
	}
	sg := core.SuggestPacks(d)
	if a.Has("json") {
		if sg == nil {
			sg = []core.Suggestion{}
		}
		printJSON(map[string]any{"project": d, "packs": sg})
		return 0
	}
	for _, s := range sg {
		outf("@%-20s (%s)\n", s.Pack, s.Reason)
	}
	if len(sg) > 0 {
		var ps []string
		for _, s := range sg {
			ps = append(ps, "@"+s.Pack)
		}
		outf("\nskill-add %s\n", strings.Join(ps, " "))
	}
	return 0
}

func cmdTaxonomy(a *Args) int {
	if a.Has("default") {
		stdout.Write(core.DefaultTaxonomyYAML())
		return 0
	}
	t := core.LoadTaxonomy()
	if a.Has("json") {
		printJSON(map[string]any{"domains": t.Domains, "errors": nzs(t.Errors)})
		return 0
	}
	for _, d := range t.Domains {
		outf("%-16s %s\n", d.ID, d.Title)
		for _, s := range d.Stacks {
			p := ""
			if len(s.Packs) > 0 {
				p = "  @" + strings.Join(s.Packs, " @")
			}
			outf("  %-18s %s%s\n", s.ID, s.Title, p)
		}
	}
	for _, e := range t.Errors {
		errfln("taxonomy: %s", e)
	}
	outf("\ncustomize: skillman taxonomy --default > %s/taxonomy.yaml (keep only what you change)\n", core.LIB)
	return 0
}

// ───────────── project ─────────────

// splitTarget: skill-add compat — a trailing positional that is a path (not a library skill) is the target dir.
func splitTarget(items []string, c string) ([]string, string) {
	if c == "" && len(items) >= 2 {
		last := items[len(items)-1]
		isSkill := core.KEBAB.MatchString(last) && core.IsDir(filepath.Join(core.SKILLS, last))
		if !strings.HasPrefix(last, "@") && !isSkill && (core.IsDir(last) || strings.Contains(last, "/") || last == "." || last == "..") {
			return items[:len(items)-1], last
		}
	}
	if c == "" {
		c = "."
	}
	return items, c
}

func cmdAdd(a *Args) int {
	if len(a.Pos) == 0 {
		return usage("skillman add <skill|@pack>… [dir] [--force] [--dry-run]")
	}
	items, target := splitTarget(a.Pos, a.Str("C", ""))
	d, err := core.ResolveProject(target, false)
	if err != nil {
		return fail(err)
	}
	its, err := core.ExpandItems(items)
	if err != nil {
		return fail(err)
	}
	if a.Has("dry-run") {
		st := stateMap(d)
		tok := 0
		type plan struct {
			Name, Pack, State string
			InLibrary         bool
			Tokens            int
		}
		var ps []plan
		for _, it := range its {
			src := filepath.Join(core.SKILLS, it.Name)
			p := plan{Name: it.Name, Pack: it.Pack, State: st[it.Name], InLibrary: core.IsFile(filepath.Join(src, "SKILL.md"))}
			if p.InLibrary {
				p.Tokens = core.SkillTokens(src)
				if core.NeedsProjectCopy(p.State) {
					tok += p.Tokens
				}
			}
			ps = append(ps, p)
		}
		tb := core.TokenBudgetOf(d)
		if a.Has("json") {
			out := []map[string]any{}
			for _, p := range ps {
				out = append(out, map[string]any{"name": p.Name, "pack": p.Pack, "state": p.State, "in_library": p.InLibrary, "tokens": p.Tokens})
			}
			printJSON(map[string]any{"project": d, "skills": out, "added_tokens": tok, "budget_after": tb.Total + tok, "budget": tb.Budget})
			return 0
		}
		for _, p := range ps {
			what := "copy"
			switch {
			case !p.InLibrary:
				what = "NOT IN LIBRARY"
			case p.State == "missing":
				what = "restore missing copy"
			case p.State != "":
				what = "already " + p.State
			}
			outf("  %-34s %-16s %3dt  %s\n", p.Name, what, p.Tokens, p.Pack)
		}
		outf("would add ≈%d tokens → %d/%d (dry run, nothing changed)\n", tok, tb.Total+tok, tb.Budget)
		return 0
	}
	rs, err := core.Install(d, its, a.Has("force"))
	if err != nil {
		return fail(err)
	}
	summary := core.SummarizeInstall(rs)
	rc := 0
	if !summary.OK() {
		rc = 1
	}
	if a.Has("json") {
		printJSON(map[string]any{"ok": summary.OK(), "results": rs, "summary": summary})
		return rc
	}
	for _, r := range rs {
		if r.OK {
			if strings.HasPrefix(r.Message, "skipped:") {
				outf("- %s: %s\n", r.Name, r.Message)
				continue
			}
			rel, _ := filepath.Rel(mustWd(), core.Dest(d, r.Name))
			outf("✓ %s: %s → %s (+ .claude/skills link)\n", r.Name, r.Message, rel)
		} else {
			fmt.Fprintf(stderr, "✗ %s: %s\n", r.Name, r.Message)
			rc = 1
		}
	}
	return rc
}

func mustWd() string { w, _ := os.Getwd(); return w }

func cmdRemove(a *Args) int {
	if len(a.Pos) == 0 {
		return usage("skillman remove <skill|@pack>… [-C dir]")
	}
	d, err := core.ResolveProject(a.Str("C", "."), false)
	if err != nil {
		return fail(err)
	}
	var all []core.Result
	for _, it := range a.Pos {
		var rs []core.Result
		if strings.HasPrefix(it, "@") {
			rs, err = core.UninstallPack(d, it[1:])
		} else {
			rs, err = core.Uninstall(d, []string{it})
		}
		if err != nil {
			return fail(err)
		}
		all = append(all, rs...)
	}
	return resultsOut(a, all, nil)
}

func cmdStatus(a *Args) int {
	d, err := projectDir(a, true)
	if err != nil {
		return fail(err)
	}
	st := core.ProjectStatus(d)
	if a.Has("json") {
		printJSON(st)
		return 0
	}
	man := core.ReadManifest(d)
	ps := strings.Join(man.PackNames(), ", ")
	if ps == "" {
		ps = "-"
	}
	outf("project: %s\npacks: %s\n", d, ps)
	for _, r := range st {
		flags := ""
		if r.Pinned {
			flags += "📌"
		}
		if !r.LinkOK && r.State != "missing" {
			flags += " link!"
		}
		via := strings.Join(r.Packs, ",")
		if via == "" && r.Manual {
			via = "manual"
		}
		outf("  %-10s %-40s %s %s\n", r.State, r.Name, via, flags)
	}
	return 0
}

func cmdDoctor(a *Args) int {
	d, err := projectDir(a, true)
	if err != nil {
		return fail(err)
	}
	res := core.Doctor(d)
	bad := false
	for _, x := range res {
		bad = bad || x.Level == "error"
	}
	if a.Has("json") {
		printJSON(map[string]any{"project": d, "ok": !bad, "checks": res})
	} else {
		icon := map[string]string{"ok": "✓", "warn": "!", "error": "✗"}
		for _, x := range res {
			outf("%s %s\n", icon[x.Level], x.Message)
		}
	}
	if bad {
		return 1
	}
	return 0
}

func cmdVerify(a *Args) int {
	d, err := projectDir(a, true)
	if err != nil {
		return fail(err)
	}
	if !core.IsFile(filepath.Join(d, ".agents", core.Manifest)) {
		return fail(fmt.Errorf("no .agents/skill-manifest.json in %s", d))
	}
	return resultsOut(a, core.Verify(d), nil)
}

func cmdAudit(a *Args) int {
	lib := a.Has("library")
	if lib && len(a.Pos) > 0 {
		return fail(fmt.Errorf("give either a target or --library, not both"))
	}
	target := "."
	if len(a.Pos) > 0 {
		target = a.Pos[0]
	}
	if !lib {
		t, err := core.ResolveProject(target, false)
		if err != nil {
			return fail(err)
		}
		target = t
	}
	rep, err := core.Audit(target, lib)
	if err != nil {
		return fail(err)
	}
	failOn := a.Str("fail-on", "review")
	rc := 0
	if core.AuditFailed(rep, failOn) {
		rc = 1
	}
	if a.Has("json") {
		printJSON(rep)
		return rc
	}
	icon := map[string]string{"ok": "✓", "info": "·", "review": "!", "error": "✗"}
	cnt := map[string]int{}
	for _, r := range rep {
		cnt[r.Level]++
		if a.Has("q") && (r.Level == "ok" || r.Level == "info") {
			continue
		}
		ex := ""
		if len(r.Executables) > 0 {
			ex = fmt.Sprintf("  (%d executable files)", len(r.Executables))
		}
		outf("%s %s%s\n", icon[r.Level], r.Name, ex)
		for _, m := range r.Errors {
			outf("    error: %s\n", m)
		}
		for _, m := range r.Review {
			outf("    review: %s\n", m)
		}
	}
	var parts []string
	for _, lv := range core.AuditLevels {
		parts = append(parts, fmt.Sprintf("%s=%d", lv, cnt[lv]))
	}
	outf("%d skills: %s\n", len(rep), strings.Join(parts, ", "))
	return rc
}

func cmdDiff(a *Args) int {
	if len(a.Pos) != 1 {
		return usage("skillman diff <skill> [-C dir]")
	}
	if err := core.CheckName(a.Pos[0]); err != nil {
		return fail(err)
	}
	d, err := core.ResolveProject(a.Str("C", "."), false)
	if err != nil {
		return fail(err)
	}
	outf("%s", core.DiffText(filepath.Join(core.SKILLS, a.Pos[0]), core.Dest(d, a.Pos[0]), "library", "project", 0))
	return 0
}

func cmdUpdate(a *Args) int {
	d, err := core.ResolveProject(a.Str("C", "."), false)
	if err != nil {
		return fail(err)
	}
	rs, err := core.UpdateProject(d, a.Pos, a.Has("force"))
	return resultsOut(a, rs, err)
}

func cmdUpdateProjects(a *Args) int {
	aff := core.ProjectsAffected(nil)
	if a.Has("dry-run") {
		if a.Has("json") {
			printJSON(map[string]any{"projects": aff})
			return 0
		}
		for _, p := range aff {
			outf("# %s: %s\n", p.Project, strings.Join(p.Skills, ", "))
		}
		return 0
	}
	var all []core.Result
	for _, p := range aff {
		if !a.Has("json") {
			outf("# %s: %s\n", p.Project, strings.Join(p.Skills, ", "))
		}
		rs, err := core.UpdateProject(p.Project, p.Skills, false)
		if err != nil {
			return fail(err)
		}
		for i := range rs {
			rs[i].Name = filepath.Base(p.Project) + "/" + rs[i].Name
		}
		all = append(all, rs...)
	}
	if len(aff) == 0 && !a.Has("json") {
		outf("no registered project has outdated skills\n")
		return 0
	}
	return resultsOut(a, all, nil)
}

func cmdAdopt(a *Args) int {
	d, err := core.ResolveProject(a.Str("C", "."), false)
	if err != nil {
		return fail(err)
	}
	rs, err := core.Adopt(d, a.Pos)
	return resultsOut(a, rs, err)
}

func cmdPin(a *Args, pin bool) int {
	if len(a.Pos) == 0 {
		return usage("skillman pin|unpin <skills…> [-C dir]")
	}
	d, err := core.ResolveProject(a.Str("C", "."), false)
	if err != nil {
		return fail(err)
	}
	rs, err := core.SetPin(d, a.Pos, pin)
	return resultsOut(a, rs, err)
}

func cmdRepairLinks(a *Args) int {
	d, err := projectDir(a, true)
	if err != nil {
		return fail(err)
	}
	msgs, err := core.RepairLinks(d)
	if err != nil {
		return fail(err)
	}
	if a.Has("json") {
		printJSON(map[string]any{"messages": nzs(msgs)})
		return 0
	}
	if len(msgs) == 0 {
		msgs = []string{"nothing to repair"}
	}
	outf("%s\n", strings.Join(msgs, "\n"))
	return 0
}

func cmdProjects(a *Args) int {
	ps := core.RegisteredProjects()
	if a.Has("json") {
		printJSON(map[string]any{"projects": nzs(ps)})
		return 0
	}
	for _, p := range ps {
		outf("%s\n", p)
	}
	return 0
}

// ───────────── setup ─────────────

func cmdBootstrap(a *Args) int {
	pack := a.Str("pack", "")
	if pack == "" || len(a.Pos) > 0 {
		return usage("skillman bootstrap --pack <core|web-vue|postgres> [--dry-run] [--allow-risky] [--json]")
	}
	items, err := core.Bootstrap(pack, a.Has("dry-run"), a.Has("allow-risky"))
	if err != nil {
		return fail(err)
	}
	ok := true
	for _, it := range items {
		if it.Status == "failed" || it.Status == "unavailable" {
			ok = false
		}
		if !a.Has("json") {
			source := ""
			if it.Source != nil {
				source = fmt.Sprintf("%s@%s:%s", it.Source.Repo, it.Source.Commit, it.Source.Subpath)
			}
			outf("%-32s %-16s %s %s\n", it.Name, it.Status, source, it.Detail)
		}
	}
	if a.Has("json") {
		printJSON(map[string]any{"pack": pack, "ok": ok, "skills": items, "dry_run": a.Has("dry-run")})
	}
	if !ok {
		return 1
	}
	return 0
}

func cmdInit(a *Args) int {
	for _, d := range []string{core.SKILLS, core.PACKS, core.STAGING} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fail(err)
		}
	}
	copied := 0
	fs.WalkDir(skillman.Assets, "packs", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		dst := filepath.Join(core.PACKS, filepath.Base(p))
		if !core.Lexists(dst) {
			b, _ := skillman.Assets.ReadFile(p)
			if os.WriteFile(dst, b, 0o644) == nil {
				copied++
			}
		}
		return nil
	})
	outf("packs: %d new starter pack(s) in %s\n", copied, core.PACKS)
	if rt := filepath.Join(core.STAGING, "repos.txt"); !core.Lexists(rt) {
		b, _ := skillman.Assets.ReadFile("staging/repos.txt")
		os.WriteFile(rt, b, 0o644)
		outf("staging/repos.txt: curated upstream list\n")
	}
	sk := filepath.Join(core.SKILLS, core.SelfSkill)
	srcJSON := filepath.Join(sk, "SOURCE.json")
	ours := !core.Lexists(sk) || strings.Contains(readOr(srcJSON), `"shipped_with": "skillman"`)
	if ours {
		b, _ := skillman.Assets.ReadFile("skills/skillman/SKILL.md")
		if readOr(filepath.Join(sk, "SKILL.md")) != string(b) {
			os.MkdirAll(sk, 0o755)
			os.WriteFile(filepath.Join(sk, "SKILL.md"), b, 0o644)
			src := core.NewOMap()
			for _, kv := range [][2]any{{"origin", "local"}, {"repo", nil}, {"subpath", nil}, {"commit", nil}, {"tier", "local"}, {"shipped_with", "skillman"}} {
				src.Set(kv[0].(string), kv[1])
			}
			core.WriteJSON(srcJSON, src)
			outf("skill 'skillman' installed in the library (agents: skillman agent-setup)\n")
		}
	}
	if gi := filepath.Join(core.LIB, ".gitignore"); !core.Lexists(gi) {
		os.WriteFile(gi, []byte(".cache/\n__pycache__/\n*.pyc\n.claude/\n.mcp.json\n"), 0o644)
	}
	if !core.IsDir(filepath.Join(core.LIB, ".git")) {
		core.Git([]string{"init", "-q"}, core.LIB, 30)
	}
	// a dangling scripts link left by skillman <= 0.1 (python) installs
	if sl := filepath.Join(core.LIB, "scripts"); core.IsSymlink(sl) && !core.Exists(sl) {
		os.Remove(sl)
		outf("removed dangling %s (python scripts are gone; use skillman)\n", sl)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		return fail(err)
	}
	outf("library ready at %s (%d skills)\n", core.LIB, len(core.LibraryNames()))
	outf("Preview starter sources: skillman bootstrap --pack core --dry-run\n")
	return 0
}

func readOr(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func cmdAgentSetup(a *Args) int {
	src := filepath.Join(core.SKILLS, core.SelfSkill)
	if !core.IsFile(filepath.Join(src, "SKILL.md")) && !a.Has("remove") {
		return fail(fmt.Errorf("%s missing: run skillman init first", src))
	}
	home, _ := os.UserHomeDir()
	var targets []string
	if !a.Has("no-claude") {
		targets = append(targets, filepath.Join(home, ".claude", "skills"))
	}
	if !a.Has("no-codex") {
		targets = append(targets, filepath.Join(home, ".agents", "skills"))
	}
	dry := a.Has("dry-run")
	rc := 0
	for _, t := range targets {
		lp := filepath.Join(t, core.SelfSkill)
		switch {
		case a.Has("remove"):
			if core.IsSymlink(lp) {
				if dst, _ := os.Readlink(lp); dst == src {
					if !dry {
						os.Remove(lp)
					}
					outf("✓ removed %s\n", lp)
					continue
				}
			}
			outf("- %s: not a skillman link, left alone\n", lp)
		case core.IsSymlink(lp):
			if dst, _ := os.Readlink(lp); dst == src {
				outf("✓ %s already links to %s\n", lp, src)
				continue
			}
			if !dry {
				os.Remove(lp)
				if err := os.Symlink(src, lp); err != nil {
					errfln("✗ %s: %v", lp, err)
					rc = 1
					continue
				}
			}
			outf("✓ relinked %s → %s\n", lp, src)
		case core.Lexists(lp):
			errfln("✗ %s exists and is not a link; leaving it (remove it to let skillman manage it)", lp)
			rc = 1
		default:
			if !dry {
				if err := os.MkdirAll(t, 0o755); err != nil {
					errfln("✗ %s: %v", t, err)
					rc = 1
					continue
				}
				if err := os.Symlink(src, lp); err != nil {
					errfln("✗ %s: %v", lp, err)
					rc = 1
					continue
				}
			}
			outf("✓ linked %s → %s\n", lp, src)
		}
	}
	if dry {
		outf("(dry run, nothing changed)\n")
	}
	return rc
}

// ───────────── skill-add (legacy entry point) ─────────────

func skillAdd(argv []string) int {
	if len(argv) == 0 || argv[0] == "-h" || argv[0] == "--help" {
		outf(`skill-add — copy skills or whole packs from the cold library into a project.
  skill-add <name|@pack>... [target-dir]   (target-dir defaults to ".")
  skill-add -C <dir> <name|@pack>...       (explicit target dir)
  skill-add --force ...                    (replace an existing, different copy; old copy -> trash)
  skill-add --list | --packs               (list library skills | packs)
  skill-add --suggest [dir]                (suggest packs from the project's stack)
Real copy  -> <dir>/.agents/skills/<name>/   (Codex, OpenCode; commit it)
Rel. link  -> <dir>/.claude/skills/<name> -> ../../.agents/skills/<name>  (Claude Code)
Tracked in <dir>/.agents/skill-manifest.json. Manage with: skillman (TUI) or skillman help
`)
		return 0
	}
	switch argv[0] {
	case "--list", "--all-list":
		for _, n := range core.LibraryNames() {
			outf("%s\n", n)
		}
		return 0
	case "--packs":
		return find("pack").run(parse([]string{"list"}))
	case "--suggest":
		return cmdSuggest(parse(argv[1:], "C"))
	}
	if !core.IsDir(core.SKILLS) {
		return fail(fmt.Errorf("library not found: %s (set SKILL_LIBRARY)", core.LIB))
	}
	return cmdAdd(parse(argv, "C"))
}
