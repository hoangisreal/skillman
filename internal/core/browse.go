package core

import (
	"sort"
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
		if s.Stack != "" {
			out[s.Stack] = true
		}
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
	State       string `json:"state"` // project state when installed
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
	HasStack     bool              `json:"-"`
	Err          error             `json:"-"`
	Project      string            `json:"project"`
	Groups       []RecGroup        `json:"groups"`
	Budget       Budget            `json:"budget"`
	ProjectState []string          `json:"installed"`
	States       map[string]string `json:"-"` // project state by skill name
	Deps         map[string]string `json:"-"` // JavaScript dependencies and tools -> evidence file
}

// RecommendationCatalog shares the same taxonomy and pack definitions between
// index classification, stack detection and pack expansion for one request.
type RecommendationCatalog struct {
	tax       *Taxonomy
	packs     map[string]*Pack
	available map[string]bool
	bySkill   map[string][]string
}

func NewRecommendationCatalog() *RecommendationCatalog {
	c := &RecommendationCatalog{LoadTaxonomy(), map[string]*Pack{}, map[string]bool{}, map[string][]string{}}
	for _, name := range ListPacks() {
		c.available[name] = true
		if pk, err := LoadPack(name); err == nil {
			c.packs[name] = pk
			for _, skill := range pk.Skills {
				c.bySkill[skill] = append(c.bySkill[skill], name)
			}
		}
	}
	return c
}

func (c *RecommendationCatalog) Index() ([]IndexRow, error) {
	if rows := loadClassifiedIndex(c.tax, c.bySkill); rows != nil {
		return rows, nil
	}
	if !IsDir(SKILLS) {
		return nil, errf("library not found: %s (run: skillman init, or set SKILL_LIBRARY)", LIB)
	}
	rows, _, _, err := rebuildIndex(true, c.tax, c.bySkill)
	return rows, err
}

// StackDomain returns the taxonomy domain of a stack ID ("" if unknown).
func (c *RecommendationCatalog) StackDomain(id string) string {
	if s := c.tax.Stack(id); s != nil {
		return s.Domain
	}
	return ""
}

func (c *RecommendationCatalog) pack(name string) (*Pack, error) {
	if p := c.packs[name]; p != nil {
		return p, nil
	}
	return nil, errf("pack not found: %s", name)
}

// ResolveRecSkill checks indexed metadata against the current library.
func ResolveRecSkill(s RecSkill) RecSkill {
	dir := SKILLS + "/" + s.Name
	if s.InLibrary && IsFile(dir+"/SKILL.md") {
		s.Tokens = SkillTokens(dir)
		if full := FullDescription(dir); full != "" {
			s.Description = full
		}
	} else {
		s.Description, s.Source, s.InLibrary = "⚠ not in library", "", false
	}
	return s
}

// Recommend proposes skills for a project: every detected pack with its curated skills, then extra library
// skills classified into the same stacks (in_pack=false), each with its state in the project and token cost.
// extra caps the non-pack skills per group (0 = none, -1 = all).
func Recommend(d string, rows []IndexRow, extra int) Recommendation {
	r := NewRecommendationCatalog().Candidates(d, rows, extra)
	for i := range r.Groups {
		for j, s := range r.Groups[i].Skills {
			r.Groups[i].Skills[j] = ResolveRecSkill(s)
		}
	}
	return r
}

// DeclaredSignals maps explicit stack IDs to local packs and packless groups.
func (c *RecommendationCatalog) DeclaredSignals(ids []string) ([]Suggestion, error) {
	var signals []Suggestion
	for _, id := range dedup(ids) {
		stack := c.tax.Stack(id)
		if stack == nil {
			return nil, errf("unknown stack %q (see: skillman taxonomy)", id)
		}
		start := len(signals)
		for _, pack := range stack.Packs {
			if c.available[pack] {
				signals = append(signals, Suggestion{Pack: pack, Stack: id, Reason: "declared:" + id})
			}
		}
		if len(signals) == start {
			signals = append(signals, Suggestion{Stack: id, Reason: "declared:" + id})
		}
	}
	return signals, nil
}

// Candidates builds groups from index metadata, leaving filesystem checks to the caller.
func (c *RecommendationCatalog) Candidates(d string, rows []IndexRow, extra int, declared ...Suggestion) Recommendation {
	tax := c.tax
	sugg := declared
	if len(sugg) == 0 {
		sugg = projectSignals(d, c.available)
	}
	status, man, err := InspectProject(d)
	if err != nil {
		return Recommendation{Project: d, Err: err}
	}
	st := StatusMap(status)
	byName := map[string]*IndexRow{}
	for i := range rows {
		byName[rows[i].Name] = &rows[i]
	}
	rec := Recommendation{Project: d, Budget: TokenBudgetOf(d), ProjectState: []string{}, States: map[string]string{}, Deps: ProjectDependencies(d)}
	for _, s := range status {
		rec.ProjectState = append(rec.ProjectState, s.Name)
		rec.States[s.Name] = s.State
	}
	for _, sg := range sugg {
		rec.HasStack = rec.HasStack || sg.Reason != "baseline"
		g := RecGroup{Pack: sg.Pack, Reason: sg.Reason, Stacks: []string{}, Skills: []RecSkill{}, Installed: man.Packs.Has(sg.Pack)}
		var members []string
		if sg.Pack != "" {
			if pk, err := c.pack(sg.Pack); err == nil {
				g.Description = pk.Description
			}
			members, _ = expandPack(sg.Pack, nil, c.pack)
		}
		stackIDs := map[string]bool{}
		if sg.Stack != "" {
			g.Stacks = []string{sg.Stack}
			if stack := tax.Stack(sg.Stack); stack != nil {
				g.Description = stack.Title
			}
			stackIDs[sg.Stack] = true
		}
		if len(declared) == 0 && sg.Pack != "" {
			for _, s := range tax.StacksForPack(sg.Pack) {
				g.Stacks = append(g.Stacks, s.ID)
				stackIDs[s.ID] = true
			}
		}
		mk := func(name string, inPack bool) RecSkill {
			r := RecSkill{Name: name, InPack: inPack, State: st[name].State}
			if row := byName[name]; row != nil && KEBAB.MatchString(name) {
				r.Description, r.Source, r.InLibrary = row.Description, row.Source, true
			} else {
				r.Description = "⚠ not in library"
			}
			return r
		}
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
	}
	if rec.Groups == nil {
		rec.Groups = []RecGroup{}
	}
	return rec
}
