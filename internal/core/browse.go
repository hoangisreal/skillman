package core

import (
	"sort"
	"strings"
)

// StackNode is one stack of the Browse tree with the skills classified into it.
type StackNode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Domain   string   `json:"domain"`
	Packs    []string `json:"packs,omitempty"`
	Skills   []string `json:"skills"`
	Detected bool     `json:"detected,omitempty"` // the current project uses this stack
}

// DomainNode is one domain of the Browse tree.
type DomainNode struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Skills   []string    `json:"skills"` // every skill in the domain (primary or via a stack)
	Stacks   []StackNode `json:"stacks"`
	Other    []string    `json:"other,omitempty"` // primary domain but no stack of this domain
	Detected bool        `json:"detected,omitempty"`
}

// BuildTree groups classified rows into domains and stacks. detected marks stack ids of the project.
func BuildTree(rows []IndexRow, tax *Taxonomy, detected map[string]bool, packs map[string][]string) []DomainNode {
	byStack := map[string][]string{}
	inDomain := map[string]map[string]bool{}
	other := map[string][]string{}
	rank := map[string]map[string]int{} // stack -> skill -> sort key
	add := func(dom, name string) {
		if inDomain[dom] == nil {
			inDomain[dom] = map[string]bool{}
		}
		inDomain[dom][name] = true
	}
	for i := range rows {
		r := &rows[i]
		hasOwn := false
		for k, sid := range r.Stacks {
			s := tax.Stack(sid)
			if s == nil {
				continue
			}
			byStack[sid] = append(byStack[sid], r.Name)
			if rank[sid] == nil {
				rank[sid] = map[string]int{}
			}
			key := k
			for _, p := range packs[r.Name] {
				if contains(s.Packs, p) {
					key = -1 // curated pack members first
				}
			}
			rank[sid][r.Name] = key
			add(s.Domain, r.Name)
			if s.Domain == r.Domain {
				hasOwn = true
			}
		}
		add(r.Domain, r.Name)
		if !hasOwn {
			other[r.Domain] = append(other[r.Domain], r.Name)
		}
	}
	sortSkills := func(sid string, xs []string) {
		sort.SliceStable(xs, func(i, j int) bool {
			a, b := rank[sid][xs[i]], rank[sid][xs[j]]
			if a != b {
				return a < b
			}
			return xs[i] < xs[j]
		})
	}
	var out []DomainNode
	emit := func(id, title string, stacks []*Stack) {
		dn := DomainNode{ID: id, Title: title, Skills: []string{}, Stacks: []StackNode{}}
		for n := range inDomain[id] {
			dn.Skills = append(dn.Skills, n)
		}
		sort.Strings(dn.Skills)
		for _, s := range stacks {
			sk := append([]string{}, byStack[s.ID]...)
			sortSkills(s.ID, sk)
			if sk == nil {
				sk = []string{}
			}
			sn := StackNode{ID: s.ID, Title: s.Title, Domain: id, Packs: s.Packs, Skills: sk, Detected: detected[s.ID]}
			dn.Detected = dn.Detected || sn.Detected
			dn.Stacks = append(dn.Stacks, sn)
		}
		dn.Other = append([]string{}, other[id]...)
		sort.Strings(dn.Other)
		if len(dn.Skills) > 0 || len(dn.Stacks) > 0 {
			out = append(out, dn)
		}
	}
	for _, d := range tax.Domains {
		emit(d.ID, d.Title, d.Stacks)
	}
	for id := range inDomain { // domains named by overrides but missing from the taxonomy
		if tax.Domain(id) == nil && id != GeneralDomain {
			emit(id, id, nil)
		}
	}
	emit(GeneralDomain, tax.DomainTitle(GeneralDomain), nil)
	return out
}

// DetectedStacks maps the packs suggested for a project to taxonomy stacks.
func DetectedStacks(sugg []Suggestion, tax *Taxonomy) map[string]bool {
	out := map[string]bool{}
	for _, s := range sugg {
		for _, st := range tax.StacksForPack(s.Pack) {
			out[st.ID] = true
		}
	}
	return out
}

// ───────────── recommend ─────────────

type RecSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	InPack      bool   `json:"in_pack"`
	InLibrary   bool   `json:"in_library"`
	State       string `json:"state,omitempty"` // project state when installed
	Tokens      int    `json:"tokens"`
}

type RecGroup struct {
	Pack        string     `json:"pack"`
	Reason      string     `json:"reason"`
	Description string     `json:"description"`
	Stacks      []string   `json:"stacks"`
	Installed   bool       `json:"pack_installed"`
	Skills      []RecSkill `json:"skills"`
}

type Recommendation struct {
	Project      string     `json:"project"`
	Groups       []RecGroup `json:"groups"`
	Budget       Budget     `json:"budget"`
	InstallHint  string     `json:"install_hint"`
	ProjectState []string   `json:"installed"`
}

// Recommend proposes skills for a project: every detected pack with its curated skills, then extra library
// skills classified into the same stacks (in_pack=false), each with its state in the project and token cost.
// extra caps the non-pack skills per group (0 = none, -1 = all).
func Recommend(d string, rows []IndexRow, extra int) Recommendation {
	tax := LoadTaxonomy()
	sugg := SuggestPacks(d)
	st := StatusMap(ProjectStatus(d))
	man := ReadManifest(d)
	byName := map[string]*IndexRow{}
	for i := range rows {
		byName[rows[i].Name] = &rows[i]
	}
	rec := Recommendation{Project: d, Budget: TokenBudgetOf(d), ProjectState: []string{}}
	for _, s := range ProjectStatus(d) {
		rec.ProjectState = append(rec.ProjectState, s.Name)
	}
	var hint []string
	for _, sg := range sugg {
		g := RecGroup{Pack: sg.Pack, Reason: sg.Reason, Stacks: []string{}, Skills: []RecSkill{}, Installed: man.Packs.Has(sg.Pack)}
		if pk, err := LoadPack(sg.Pack); err == nil {
			g.Description = pk.Description
		}
		stackIDs := map[string]bool{}
		for _, s := range tax.StacksForPack(sg.Pack) {
			g.Stacks = append(g.Stacks, s.ID)
			stackIDs[s.ID] = true
		}
		mk := func(name string, inPack bool) RecSkill {
			r := RecSkill{Name: name, InPack: inPack, State: st[name].State}
			if row := byName[name]; row != nil {
				r.Description, r.Source, r.InLibrary = row.Description, row.Source, true
			} else {
				r.Description = "⚠ not in library"
			}
			r.Tokens = SkillTokens(SKILLS + "/" + name)
			return r
		}
		members, _ := ExpandPack(sg.Pack)
		seen := map[string]bool{}
		for _, m := range members {
			seen[m] = true
			g.Skills = append(g.Skills, mk(m, true))
		}
		n := 0
		if extra != 0 {
			for i := range rows {
				r := &rows[i]
				if seen[r.Name] {
					continue
				}
				for _, sid := range r.Stacks {
					if stackIDs[sid] {
						if extra < 0 || n < extra {
							g.Skills = append(g.Skills, mk(r.Name, false))
							n++
						}
						seen[r.Name] = true
						break
					}
				}
			}
		}
		rec.Groups = append(rec.Groups, g)
		if !g.Installed {
			hint = append(hint, "@"+sg.Pack)
		}
	}
	if rec.Groups == nil {
		rec.Groups = []RecGroup{}
	}
	if len(hint) > 0 {
		rec.InstallHint = "skillman add " + strings.Join(hint, " ")
	}
	return rec
}
