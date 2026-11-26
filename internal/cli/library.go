package cli

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hoangisreal/skillman/internal/core"
)

// ───────────── packs ─────────────

func cmdPack(a *Args) int {
	if len(a.Pos) == 0 {
		return helpFor("pack")
	}
	sub, rest := a.Pos[0], a.Pos[1:]
	name := ""
	if len(rest) > 0 {
		name = strings.TrimPrefix(rest[0], "@")
	}
	need := func(n int, u string) bool {
		if len(rest) < n {
			usage("skillman pack " + u)
			return false
		}
		return true
	}
	ok := func(msg string) int {
		if a.Has("json") {
			printJSON(map[string]any{"ok": true, "message": msg})
		} else {
			outf("✓ %s\n", msg)
		}
		return 0
	}
	switch sub {
	case "list", "ls":
		sums := core.PackSummaries(a.Has("vendor") || a.Has("all"))
		if a.Has("json") {
			if sums == nil {
				sums = []core.PackSummary{}
			}
			printJSON(map[string]any{"packs": sums})
			return 0
		}
		for _, s := range sums {
			if s.Error != "" && s.Count == 0 {
				outf("@%-28s ERROR %s\n", s.Name, s.Error)
				continue
			}
			ext := ""
			if len(s.Extends) > 0 {
				ext = " (extends " + strings.Join(s.Extends, ", ") + ")"
			}
			outf("@%-28s %3d skills  %s%s\n", s.Name, s.Count, s.Description, ext)
			if a.Has("v") {
				all, _ := core.ExpandPack(s.Name)
				outf("    %s\n", strings.Join(all, " "))
			}
		}
		if !a.Has("vendor") {
			outf("(vendor packs hidden: --vendor)\n")
		}
		return 0
	case "show":
		if !need(1, "show <pack>") {
			return 2
		}
		pk, err := core.LoadPack(name)
		if err != nil {
			return fail(err)
		}
		all, err := core.ExpandPack(name)
		if err != nil {
			return fail(err)
		}
		rows, _ := loadIndex()
		byName := map[string]core.IndexRow{}
		for _, r := range rows {
			byName[r.Name] = r
		}
		tok := 0
		type mem struct {
			Name        string `json:"name"`
			Via         string `json:"via,omitempty"`
			Description string `json:"description"`
			InLibrary   bool   `json:"in_library"`
			Tokens      int    `json:"tokens"`
		}
		var ms []mem
		for _, s := range all {
			r, in := byName[s]
			m := mem{Name: s, Description: r.Description, InLibrary: in}
			if !core.Contains(pk.Skills, s) {
				m.Via = "extends"
			}
			if in {
				m.Tokens = core.SkillTokens(filepath.Join(core.SKILLS, s))
				tok += m.Tokens
			}
			ms = append(ms, m)
		}
		if a.Has("json") {
			printJSON(map[string]any{"pack": pk, "skills": ms, "tokens": tok, "install": "skillman add @" + name})
			return 0
		}
		outf("@%s: %s\nextends: %s · %d skills · ≈%d tokens · install: skillman add @%s\n", name, pk.Description,
			orDash(strings.Join(pk.Extends, ", ")), len(all), tok, name)
		for _, m := range ms {
			d := m.Description
			if !m.InLibrary {
				d = "⚠ not in library"
			}
			v := ""
			if m.Via != "" {
				v = " (via extends)"
			}
			outf("  %-34s %s%s\n", m.Name, short(d, 80), v)
		}
		return 0
	case "create", "new":
		if !need(1, "create <pack> [skills…] [-d DESC] [--extends a,b]") {
			return 2
		}
		if p, err := core.PackPath(name); err != nil {
			return fail(err)
		} else if core.Exists(p) {
			return fail(fmt.Errorf("pack exists: %s", name))
		}
		pk := &core.Pack{Name: name, Description: a.Str("d", a.Str("description", "")), Extends: a.List("extends"), Skills: rest[1:]}
		if err := core.SavePack(pk); err != nil {
			return fail(err)
		}
		return ok(fmt.Sprintf("created @%s (%d skills)%s", name, len(pk.Skills), commitHint()))
	case "delete", "rm-pack":
		if !need(1, "delete <pack>") {
			return 2
		}
		slot, err := core.DeletePack(name)
		if err != nil {
			return fail(err)
		}
		return ok(fmt.Sprintf("@%s moved to trash: %s", name, slot))
	case "rename", "mv":
		if !need(2, "rename <old> <new>") {
			return 2
		}
		if err := core.RenamePack(name, strings.TrimPrefix(rest[1], "@")); err != nil {
			return fail(err)
		}
		return ok(fmt.Sprintf("renamed @%s → @%s%s", name, strings.TrimPrefix(rest[1], "@"), commitHint()))
	case "describe":
		if !need(1, "describe <pack> <description…>") {
			return 2
		}
		pk, err := core.LoadPack(name)
		if err != nil {
			return fail(err)
		}
		pk.Description = strings.Join(rest[1:], " ")
		if err := core.SavePack(pk); err != nil {
			return fail(err)
		}
		return ok("description of @" + name + " updated")
	case "extends":
		if !need(1, "extends <pack> [packs…]") {
			return 2
		}
		pk, err := core.LoadPack(name)
		if err != nil {
			return fail(err)
		}
		pk.Extends = nil
		for _, e := range rest[1:] {
			for _, x := range strings.Split(e, ",") {
				if x = strings.TrimPrefix(strings.TrimSpace(x), "@"); x != "" {
					pk.Extends = append(pk.Extends, x)
				}
			}
		}
		if err := core.SavePack(pk); err != nil {
			return fail(err)
		}
		return ok(fmt.Sprintf("@%s extends: %s", name, orDash(strings.Join(pk.Extends, ", "))))
	case "add", "rm", "remove":
		if !need(2, sub+" <pack> <skills…>") {
			return 2
		}
		pk, err := core.LoadPack(name)
		if err != nil {
			return fail(err)
		}
		lib := map[string]bool{}
		for _, n := range core.LibraryNames() {
			lib[n] = true
		}
		var changed, warn []string
		for _, s := range rest[1:] {
			if sub == "add" {
				if !core.Contains(pk.Skills, s) {
					pk.Skills = append(pk.Skills, s)
					changed = append(changed, s)
					if !lib[s] {
						warn = append(warn, s)
					}
				}
			} else if core.Contains(pk.Skills, s) {
				var keep []string
				for _, x := range pk.Skills {
					if x != s {
						keep = append(keep, x)
					}
				}
				pk.Skills = keep
				changed = append(changed, s)
			}
		}
		if err := core.SavePack(pk); err != nil {
			return fail(err)
		}
		verb := map[string]string{"add": "added to", "rm": "removed from", "remove": "removed from"}[sub]
		msg := fmt.Sprintf("%d skill(s) %s @%s: %s%s", len(changed), verb, name, orDash(strings.Join(changed, " ")), commitHint())
		if len(warn) > 0 {
			msg += "\n! not in the library yet: " + strings.Join(warn, " ")
		}
		return ok(msg)
	}
	return helpFor("pack")
}

func commitHint() string {
	return fmt.Sprintf(" (commit: git -C %s commit packs)", core.LIB)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func cmdExpand(a *Args) int {
	for _, n := range a.Pos {
		all, err := core.ExpandPack(strings.TrimPrefix(n, "@"))
		if err != nil {
			return fail(err)
		}
		outf("%s\n", strings.Join(all, "\n"))
	}
	return 0
}

func cmdValidatePacks(a *Args) int {
	probs := core.ValidatePacks()
	for _, p := range probs {
		outf("✗ %s: %s\n", p.Pack, p.Problem)
	}
	covered := map[string]bool{}
	for _, n := range core.ListPacks() {
		if strings.HasPrefix(n, "vendor/") {
			continue
		}
		if pk, err := core.LoadPack(n); err == nil {
			for _, s := range pk.Skills {
				covered[s] = true
			}
		}
	}
	lib := core.LibraryNames()
	c := 0
	for _, s := range lib {
		if covered[s] {
			c++
		}
	}
	outf("%d packs · stack packs cover %d/%d skills · problems: %d\n", len(core.ListPacks()), c, len(lib), len(probs))
	if len(probs) > 0 {
		return 1
	}
	return 0
}

// ───────────── library / updates ─────────────

func progressLine(json bool) func(string, int, int) {
	if json {
		return nil
	}
	return func(repo string, done, total int) { fmt.Fprintf(stderr, "  checked %s (%d/%d)\n", repo, done, total) }
}

func printUpdateReport(a *Args, rep *core.UpdateReport, mode string) int {
	rc := 0
	for _, result := range rep.Results {
		if !result.OK {
			rc = 1
		}
	}
	if len(rep.Errors) > 0 {
		rc = 1
	}
	if a.Has("json") {
		printJSON(rep)
		return rc
	}
	cnt := map[string]int{}
	for _, it := range rep.Items {
		cnt[it.Status]++
	}
	var keys []string
	for k := range cnt {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, cnt[k]))
	}
	outf("summary: %s, no-upstream=%d\n", strings.Join(parts, ", "), len(rep.NoUpstream))
	for _, it := range rep.Items {
		if it.Status == "up-to-date" {
			continue
		}
		outf("  %-16s %-40s %s %s→%s\n", it.Status, it.Name, it.Repo, core.Short(it.Commit, 8), core.Short(it.NewCommit, 8))
		if a.Has("log") && it.Status == "changed" {
			outf("      %s\n", strings.ReplaceAll(core.Changelog(it, 30), "\n", "\n      "))
		}
	}
	for _, s := range rep.Staged {
		if len(s.Risk) > 0 {
			outf("  RISKY %s: %s\n", s.Name, strings.Join(s.Risk, "; "))
		}
	}
	for _, e := range rep.Errors {
		errfln("  error: %s", e)
	}
	printResults(rep.Results)
	if aff := core.ProjectsAffected(nil); len(aff) > 0 && mode != "check" {
		outf("projects with outdated skills (run: skillman update-projects):\n")
		for _, p := range aff {
			outf("  %s: %s\n", p.Project, strings.Join(p.Skills, ", "))
		}
	}
	return rc
}

func libNames(a *Args, rest []string) ([]string, error) {
	names := append([]string{}, rest...)
	for _, p := range a.List("pack") {
		all, err := core.ExpandPack(strings.TrimPrefix(p, "@"))
		if err != nil {
			return nil, err
		}
		names = append(names, all...)
	}
	for _, n := range names {
		if err := core.CheckName(n); err != nil {
			return nil, err
		}
	}
	return names, nil
}

func cmdLibrary(a *Args) int {
	if len(a.Pos) == 0 {
		return helpFor("library")
	}
	sub, rest := a.Pos[0], a.Pos[1:]
	switch sub {
	case "check", "apply", "auto-safe":
		names, err := libNames(a, rest)
		if err != nil {
			return fail(err)
		}
		rep := core.UpdateLibrary(names, sub, a.Has("allow-risky"), !a.Has("no-commit"), progressLine(a.Has("json")))
		return printUpdateReport(a, rep, sub)
	case "duplicates":
		type pair struct {
			A       string `json:"a"`
			B       string `json:"b"`
			ATokens int    `json:"a_tokens"`
			BTokens int    `json:"b_tokens"`
			Reason  string `json:"reason"`
		}
		pairs := []pair{}
		for name, sims := range core.SimilarSkills(core.LoadIndex(), nil) {
			for _, s := range sims {
				if name < s.Name {
					pairs = append(pairs, pair{name, s.Name, core.SkillTokens(filepath.Join(core.SKILLS, name)), core.SkillTokens(filepath.Join(core.SKILLS, s.Name)), s.Reason})
				}
			}
		}
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].A+pairs[i].B < pairs[j].A+pairs[j].B })
		if a.Has("json") {
			printJSON(map[string]any{"duplicates": pairs})
			return 0
		}
		for _, p := range pairs {
			outf("%-32s %4dt  ~  %-32s %4dt  (%s)\n", p.A, p.ATokens, p.B, p.BTokens, p.Reason)
		}
		if len(pairs) == 0 {
			outf("no near-duplicate skills\n")
		}
		return 0
	case "pending":
		p := core.LoadPending()
		if a.Has("json") {
			if p == nil {
				p = []core.Staged{}
			}
			printJSON(map[string]any{"pending": p})
			return 0
		}
		if len(p) == 0 {
			outf("no risky updates waiting\n")
		}
		for _, s := range p {
			outf("%-34s %s %s→%s\n    risk: %s\n", s.Name, s.Repo, core.Short(s.Commit, 8), core.Short(s.NewCommit, 8), strings.Join(s.Risk, "; "))
		}
		return 0
	case "rollback":
		if len(rest) != 1 {
			return usage("skillman library rollback <skill>")
		}
		msg, err := core.Rollback(rest[0], true)
		if err != nil {
			return fail(err)
		}
		outf("%s\n", msg)
		return 0
	case "pin", "unpin":
		if len(rest) == 0 {
			return usage("skillman library pin|unpin <skills…>")
		}
		var rs []core.Result
		for _, n := range rest {
			if err := core.SetLibraryPin(n, sub == "pin"); err != nil {
				rs = append(rs, core.Result{Name: n, OK: false, Message: err.Error()})
			} else {
				rs = append(rs, core.Result{Name: n, OK: true, Message: sub + "ned"})
			}
		}
		return resultsOut(a, rs, nil)
	case "rebuild-index", "validate":
		rows, errs, warns, err := core.RebuildIndex(!a.Has("no-index"))
		if err != nil {
			return fail(err)
		}
		probs := core.ValidatePacks()
		if a.Has("json") {
			printJSON(map[string]any{"skills": len(rows), "errors": len(errs), "warnings": len(warns), "pack_problems": probs})
		} else {
			outf("skills=%d errors=%d warnings=%d  (details: %s)\n", len(rows), len(errs), len(warns), filepath.Join(core.LIB, "VALIDATION.md"))
			var missing, other []core.PackProblem
			for _, p := range probs {
				if strings.HasPrefix(p.Problem, "skill not in library") {
					missing = append(missing, p)
				} else {
					other = append(other, p)
				}
			}
			for _, p := range other {
				outf("pack problem: %s: %s\n", p.Pack, p.Problem)
			}
			if len(missing) > 0 {
				ps := map[string]bool{}
				for _, p := range missing {
					ps[p.Pack] = true
				}
				outf("packs reference %d skill(s) not in the library yet (%d packs). Start with skillman bootstrap --pack core --dry-run or review sources with skillman upstream verify.\n", len(missing), len(ps))
				if a.Has("v") {
					for _, p := range missing {
						outf("  %s: %s\n", p.Pack, p.Problem)
					}
				}
			}
		}
		if sub == "validate" && a.Has("strict") && (len(errs) > 0 || len(probs) > 0) {
			return 1
		}
		return 0
	case "new":
		if len(rest) != 1 {
			return usage("skillman library new <skill> [-d DESC]")
		}
		d, err := core.NewSkill(rest[0], a.Str("d", a.Str("description", "")))
		if err != nil {
			return fail(err)
		}
		outf("✓ created %s/SKILL.md — edit it, then: skillman add %s\n", d, rest[0])
		return 0
	case "build-packs":
		n, err := core.BuildVendorPacks()
		if err != nil {
			return fail(err)
		}
		outf("vendor packs: %d (packs/vendor/*.yaml)\n", n)
		return 0
	}
	return helpFor("library")
}

// update-library --check|--apply|--auto-safe (skillcore.py syntax)
func cmdUpdateLibraryCompat(a *Args) int {
	mode := "check"
	if a.Has("apply") {
		mode = "apply"
	} else if a.Has("auto-safe") {
		mode = "auto-safe"
	}
	names, err := libNames(a, a.Pos)
	if err != nil {
		return fail(err)
	}
	rep := core.UpdateLibrary(names, mode, a.Has("allow-risky"), !a.Has("no-commit"), progressLine(a.Has("json")))
	return printUpdateReport(a, rep, mode)
}

// ───────────── upstream import pipeline ─────────────

func cmdUpstream(a *Args) int {
	if len(a.Pos) == 0 {
		return helpFor("upstream")
	}
	sub, rest := a.Pos[0], a.Pos[1:]
	switch sub {
	case "verify":
		file := ""
		if len(rest) > 0 {
			file = rest[0]
		}
		res, err := core.VerifyRepos(file, a.Int("j", 8), func(r core.RepoResult) {
			if !a.Has("json") {
				outf("%-16s %-55s %4d skills\n", r.Status, r.Repo, len(r.Skills))
			}
		})
		if err != nil {
			return fail(err)
		}
		ok, total := 0, 0
		for _, r := range res {
			if len(r.Skills) > 0 {
				ok++
				total += len(r.Skills)
			}
		}
		if a.Has("json") {
			printJSON(map[string]any{"repos": len(res), "with_skills": ok, "candidates": total})
		} else {
			outf("\n%d/%d repos with skills, %d candidates -> %s\n", ok, len(res), total, filepath.Join(core.STAGING, "CANDIDATES.md"))
		}
		return 0
	case "candidates":
		cs := core.ReadCandidates()
		lib := map[string]bool{}
		for _, n := range core.LibraryNames() {
			lib[n] = true
		}
		q := strings.ToLower(strings.Join(rest, " "))
		var out []core.Candidate
		for _, c := range cs {
			if a.Has("ticked") && !c.Ticked || a.Has("new") && lib[c.Name] {
				continue
			}
			hay := strings.ToLower(c.Name + " " + c.Repo + " " + c.Description)
			okq := true
			for _, w := range strings.Fields(q) {
				okq = okq && strings.Contains(hay, w)
			}
			if okq {
				out = append(out, c)
			}
		}
		if a.Has("json") {
			if out == nil {
				out = []core.Candidate{}
			}
			printJSON(map[string]any{"candidates": out})
			return 0
		}
		for _, c := range out {
			x := " "
			if c.Ticked {
				x = "x"
			}
			in := ""
			if lib[c.Name] {
				in = " (in library)"
			}
			outf("[%s] %-5d %-34s %-40s %s%s\n", x, c.Line, c.Name, c.Repo, short(c.Description, 60), in)
		}
		return 0
	case "tick":
		if lines := a.List("line"); len(lines) > 0 {
			if len(rest) > 0 {
				return usage("skillman upstream tick --line N")
			}
			ch := map[int]bool{}
			for _, l := range lines {
				n, err := strconv.Atoi(l)
				if err != nil {
					return usage("skillman upstream tick --line N")
				}
				ch[n] = !a.Has("untick")
			}
			if err := core.SetTicks(ch); err != nil {
				return fail(err)
			}
			outf("✓ %d line(s) %s\n", len(ch), map[bool]string{true: "unticked", false: "ticked"}[a.Has("untick")])
			return 0
		}
		o := core.TickOptions{Tier: a.Str("tier", ""), MaxPerRepo: a.Int("max-per-repo", 0), Reset: a.Has("reset")}
		if a.Has("repo") {
			o.Repos = a.List("repo")
		}
		if a.Has("pack") {
			o.Packs = append(a.List("pack"), rest...)
			if len(o.Packs) == 0 {
				return usage("skillman upstream tick --pack <name>…")
			}
		} else if len(rest) > 0 {
			return usage("skillman upstream tick [--pack <name>…] [--repo <owner/repo>]")
		}
		msg, err := core.Tick(o)
		if err != nil {
			return fail(err)
		}
		outf("%s\n", msg)
		return 0
	case "import":
		dry := a.Has("dry-run")
		opts := core.QueueImportOptions{DryRun: dry, Update: a.Has("update"), RenameCollisions: a.Has("rename-collisions"), AllowRisky: a.Has("allow-risky")}
		log, err := core.ImportTicked(opts, func(l core.ImportLine) {
			if !a.Has("json") {
				outf("%-9s %s:%s  %s\n", l.Result, l.Repo, l.Name, l.Detail)
			}
		})
		if err != nil {
			return fail(err)
		}
		if log == nil {
			log = []core.ImportLine{}
			if !a.Has("json") {
				outf("nothing ticked in staging/CANDIDATES.md\n")
			}
		}
		rc, changed := 0, false
		for _, l := range log {
			changed = changed || l.Result == "IMPORTED" || l.Result == "UPDATED"
			if l.Result == "FAILED" {
				rc = 1
			}
		}
		if changed {
			rows, e, w, err := core.RebuildIndex(true)
			if err != nil {
				return fail(err)
			}
			if !a.Has("json") {
				outf("validate: skills=%d errors=%d warnings=%d\nreview, then commit: git -C %s add -A && git -C %s commit\n", len(rows), len(e), len(w), core.LIB, core.LIB)
			}
		}
		if a.Has("json") {
			printJSON(map[string]any{"dry_run": dry, "results": log})
		}
		return rc
	}
	return helpFor("upstream")
}

// ───────────── trash ─────────────

func cmdTrash(a *Args) int {
	sub := "list"
	if len(a.Pos) > 0 {
		sub = a.Pos[0]
	}
	project := ""
	if a.Has("C") {
		if a.Str("C", "") == "" {
			return usage("skillman trash " + sub + " [-C dir]")
		}
		var err error
		project, err = core.ResolveProject(a.Str("C", ""), false)
		if err != nil {
			return fail(err)
		}
	}
	switch sub {
	case "list":
		es := core.ListTrash(project)
		if a.Has("json") {
			if es == nil {
				es = []core.TrashEntry{}
			}
			printJSON(map[string]any{"entries": es})
			return 0
		}
		for _, e := range es {
			outf("%s  %-5s %-35s %s\n", e.S("id"), e.S("kind"), e.S("name"), e.S("project"))
		}
		return 0
	case "restore":
		if len(a.Pos) != 2 {
			return usage("skillman trash restore <id>")
		}
		for _, e := range core.ListTrash(project) {
			if e.S("id") == a.Pos[1] {
				msg, err := core.RestoreTrash(e.S("slot"))
				if err != nil {
					return fail(err)
				}
				if a.Has("json") {
					printJSON(map[string]any{"id": e.S("id"), "message": msg})
				} else {
					outf("%s\n", msg)
				}
				return 0
			}
		}
		return fail(fmt.Errorf("no trash entry %s", a.Pos[1]))
	case "empty":
		if !a.Has("yes") {
			return fail(fmt.Errorf("refusing to empty trash without --yes"))
		}
		slots := []string{}
		for _, e := range core.ListTrash(project) {
			if len(a.Pos) == 1 || core.Contains(a.Pos[1:], e.S("id")) {
				slots = append(slots, e.S("slot"))
			}
		}
		if len(a.Pos) > 1 && len(slots) == 0 {
			return fail(fmt.Errorf("no matching trash entries"))
		}
		deleted, err := core.EmptyTrash(slots)
		if a.Has("json") {
			report := map[string]any{"deleted": deleted}
			if err != nil {
				report["error"] = err.Error()
			}
			printJSON(report)
		} else {
			outf("deleted %d trash entries\n", deleted)
		}
		if err != nil {
			errfln("skillman: %v", err)
			return 1
		}
		return 0
	}
	return helpFor("trash")
}
