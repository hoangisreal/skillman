package search

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/hoangisreal/skillman/internal/core"
)

type Choice struct {
	core.RecSkill
	Reason      string   `json:"reason"`
	Evidence    string   `json:"evidence"`
	AddedTokens int      `json:"added_tokens"`
	Matched     []string `json:"matched_keywords"`
	// Signals say why the skill was chosen, also when there is no task:
	// its pack or stack, a matching dependency, a name match, an installed copy.
	Signals    []string `json:"signals"`
	score      int
	group      int
	stackMatch int
	offStack   bool
	// affinity: 2 the name matches the group's pack or stack, 1 the project
	// depends on the tool the skill is for, -1 the skill names a tool the
	// project does not use.
	affinity int
}

// techDomains hold stacks that tie a skill to one technology; a project on
// React gains nothing from a Vue or Flutter skill outside its stacks.
var techDomains = map[string]bool{
	"web-frontend": true, "backend": true, "mobile": true, "languages": true, "desktop": true, "web3": true,
	"data": true, "ml-ai": true,
}

// commonWords never count as naming a technology in a task.
var commonWords = map[string]bool{
	"add": true, "adding": true, "api": true, "app": true, "apps": true, "best": true, "build": true, "create": true,
	"data": true, "debug": true, "deploy": true, "deployment": true, "docs": true, "guide": true, "migration": true,
	"migrations": true, "patterns": true, "planner": true, "practices": true, "review": true, "setup": true,
	"simple": true, "test": true, "testing": true, "tests": true, "tool": true, "tools": true, "unit": true,
}

// namesSkillTech reports whether the task names the technology in a skill's
// name, e.g. "deploy with vllm" for vllm-deploy-docker.
func namesSkillTech(name string, words []string) bool {
	for _, seg := range strings.Split(name, "-") {
		if len(seg) > 2 && !commonWords[seg] && slices.ContainsFunc(words, func(w string) bool { return alnum(w) == seg }) {
			return true
		}
	}
	return false
}

// genericTokens never count as a name match between a skill and its group.
var genericTokens = map[string]bool{
	"web": true, "core": true, "app": true, "apps": true, "dev": true, "quality": true, "best": true,
	"practices": true, "general": true, "workflow": true, "build": true, "test": true, "tests": true,
	"data": true, "backend": true, "frontend": true, "stack": true,
}

func groupTokens(g core.RecGroup) []string {
	var out []string
	for _, id := range append([]string{g.Pack}, g.Stacks...) {
		for _, t := range strings.Split(id, "-") {
			if len(t) >= 3 && !genericTokens[t] {
				out = append(out, t)
			}
		}
	}
	return out
}

func alnum(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
}

// otherTech reports whether a skill's stacks tie it to a technology the
// project does not use while the project's own technology is known.
func otherTech(catalog *core.RecommendationCatalog, stacks []string, detected map[string]bool) bool {
	known := false
	for s := range detected {
		known = known || techDomains[catalog.StackDomain(s)]
	}
	tied := false
	for _, s := range stacks {
		if techDomains[catalog.StackDomain(s)] {
			if detected[s] {
				return false
			}
			tied = true
		}
	}
	return known && tied
}

// nameToken returns the group token that a segment of the skill name starts with.
func nameToken(name string, tokens []string) string {
	for _, seg := range strings.Split(name, "-") {
		for _, t := range tokens {
			if strings.HasPrefix(seg, t) {
				return t
			}
		}
	}
	return ""
}

// applyEvidence sets affinity and signals from project dependencies and names.
// A tool the project does not use counts against the skill unless the task names it.
func (c *Choice) applyEvidence(deps map[string]string, tokens, words []string) {
	use := core.ToolEvidence(c.Name, deps)
	for _, u := range use.Used {
		c.Signals = append(c.Signals, "dependency "+u)
	}
	missing := use.Missing[:0:0]
	for _, m := range use.Missing {
		if !slices.ContainsFunc(words, func(w string) bool {
			w = alnum(w) // "next.js" names nextjs
			return w != "" && (strings.HasPrefix(m, w) || strings.HasPrefix(w, m))
		}) {
			missing = append(missing, m)
		}
	}
	switch {
	case len(missing) > 0:
		c.affinity = -1
	case nameToken(c.Name, tokens) != "":
		c.affinity = 2
		c.Signals = append(c.Signals, "name matches "+nameToken(c.Name, tokens))
	case len(use.Used) > 0:
		c.affinity = 1
	}
	if c.State == "ok" {
		c.Signals = append(c.Signals, "installed")
	}
}

// rank halves the score of a skill outside the project's stacks, so a stack
// match wins over an equally good match elsewhere.
func (c Choice) rank() int {
	if c.offStack {
		return c.score / 2
	}
	return c.score
}

// stopWords never count as task matches.
var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "for": true, "to": true, "of": true,
	"in": true, "on": true, "at": true, "by": true, "with": true, "from": true, "into": true, "my": true,
	"our": true, "your": true, "this": true, "that": true, "is": true, "are": true, "be": true, "how": true,
	"i": true, "me": true, "we": true, "it": true,
}

func taskWords(task string) []string {
	all := strings.Fields(strings.ToLower(task))
	var words []string
	for _, w := range all {
		if !stopWords[w] {
			words = append(words, w)
		}
	}
	// Stop words alone are not a task: recommend for the project instead.
	return words
}

type Recommendation struct {
	core.Recommendation
	Shortlist   []Choice `json:"shortlist"`
	InstallArgs []string `json:"install_args"` // arguments after the executable
	InstallHint string   `json:"install_hint"`
	// InstallTokens is the description cost that InstallArgs would add.
	InstallTokens int    `json:"install_tokens"`
	Message       string `json:"message,omitempty"`
}

// Match most of a task's keywords. A two-word query still needs both words,
// while a longer request can include a verb absent from the skill metadata.
func taskMatch(doc Doc, words []string) (int, []string) {
	score := 0
	var matched []string
	for _, word := range words {
		points := doc.score([]string{word})
		if points == 0 && len(word) > 4 && strings.HasSuffix(word, "s") {
			points = doc.score([]string{strings.TrimSuffix(word, "s")})
		}
		if points > 0 {
			score += points
			matched = append(matched, word)
		}
	}
	if 3*len(matched) < 2*len(words) {
		return 0, nil
	}
	return score, matched
}

// Recommend ranks the entire local stack before truncating the detailed group view.
func Recommend(project string, rows []core.IndexRow, task string, limit, extra int) Recommendation {
	return Rank(core.NewRecommendationCatalog(), project, rows, task, limit, extra, false)
}

func Rank(catalog *core.RecommendationCatalog, project string, rows []core.IndexRow, task string, limit, extra int, brief bool, declared ...core.Suggestion) Recommendation {
	r := Recommendation{Recommendation: catalog.Candidates(project, rows, -1, declared...), Shortlist: []Choice{}, InstallArgs: []string{}}
	if r.Err != nil {
		r.Message = r.Err.Error()
		return r
	}
	words := taskWords(task)
	var ranked []Choice
	inGroups := map[string]bool{}
	// Only resolve a name once, even when several groups contain it.
	resolved := map[string]core.RecSkill{}
	resolve := func(s core.RecSkill) core.RecSkill {
		if hit, ok := resolved[s.Name]; ok {
			hit.InPack = s.InPack
			return hit
		}
		s = core.ResolveRecSkill(s)
		resolved[s.Name] = s
		return s
	}
	detected := map[string]bool{}
	for _, g := range r.Groups {
		if g.Reason == "baseline" {
			continue
		}
		for _, s := range g.Stacks {
			detected[s] = true
		}
	}
	rowStacks := map[string][]string{}
	for _, row := range rows {
		rowStacks[row.Name] = row.Stacks
	}
	for i := range r.Groups {
		g := &r.Groups[i]
		visible := g.Skills[:0]
		extras := 0
		for _, s := range g.Skills {
			inGroups[s.Name] = true
			if !brief && (s.InPack || extra < 0 || extras < extra) {
				visible = append(visible, resolve(s))
				if !s.InPack {
					extras++
				}
			}
			if !s.InLibrary {
				continue
			}
			c := Choice{RecSkill: s, group: i, Evidence: g.Reason, Reason: strings.Join(g.Stacks, ", "), Matched: []string{}}
			if g.Reason != "baseline" {
				c.stackMatch = 1
				if g.Pack != "web-quality" || strings.HasPrefix(g.Reason, "declared:") {
					c.stackMatch = 2
				}
			}
			if core.NeedsProjectCopy(s.State) {
				c.AddedTokens = core.DescriptionTokens(s.Name, s.Description)
			}
			if s.InPack {
				c.Reason = "@" + g.Pack
				c.Signals = []string{"pack @" + g.Pack + " (" + g.Reason + ")"}
			} else {
				c.Signals = []string{"stack " + strings.Join(g.Stacks, ", ") + " (" + g.Reason + ")"}
			}
			c.applyEvidence(r.Deps, groupTokens(*g), words)
			// Never suggest a skill for a tool or technology the project does not
			// use, unless the task names it. Curated pack members are kept.
			if c.affinity < 0 || (!s.InPack && otherTech(catalog, rowStacks[s.Name], detected) && !namesSkillTech(s.Name, words)) {
				continue
			}
			if len(words) > 0 {
				doc := Doc{name: s.Name, desc: strings.ToLower(s.Description), src: strings.ToLower(s.Source)}
				score, matched := taskMatch(doc, words)
				if score == 0 {
					continue
				}
				c.score, c.Matched = score, matched
				c.Reason += " · " + strings.Join(matched, ", ")
				c.Signals = append(c.Signals, "task keywords "+strings.Join(matched, ", "))
			}
			ranked = append(ranked, c)
		}
		g.Skills = visible
	}
	// A task can name work outside the detected stacks: rank the rest of the library too.
	// Declared stacks are an explicit scope and stay closed.
	if len(words) > 0 && len(declared) == 0 {
		for _, row := range rows {
			if inGroups[row.Name] || !core.KEBAB.MatchString(row.Name) {
				continue
			}
			inGroups[row.Name] = true
			doc := Doc{name: row.Name, desc: strings.ToLower(row.Description), src: strings.ToLower(row.Source)}
			score, matched := taskMatch(doc, words)
			if score == 0 {
				continue
			}
			s := core.RecSkill{Name: row.Name, Description: row.Description, Source: row.Source, InLibrary: true, State: r.States[row.Name]}
			c := Choice{RecSkill: s, group: -1, offStack: true, Evidence: "library", score: score, Matched: matched, Signals: []string{"task keywords " + strings.Join(matched, ", ")}}
			c.applyEvidence(r.Deps, nil, words)
			if c.affinity < 0 || (otherTech(catalog, row.Stacks, detected) && !namesSkillTech(row.Name, words)) {
				continue
			}
			c.Reason = strings.Join(matched, ", ")
			if len(row.Stacks) > 0 {
				c.Reason = strings.Join(row.Stacks, ", ") + " · " + c.Reason
			}
			if core.NeedsProjectCopy(s.State) {
				c.AddedTokens = core.DescriptionTokens(s.Name, s.Description)
			}
			ranked = append(ranked, c)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if len(a.Matched) != len(b.Matched) {
			return len(a.Matched) > len(b.Matched)
		}
		if a.rank() != b.rank() {
			return a.rank() > b.rank()
		}
		if a.score != b.score {
			return a.score > b.score
		}
		if a.affinity != b.affinity {
			return a.affinity > b.affinity
		}
		if a.stackMatch != b.stackMatch {
			return a.stackMatch > b.stackMatch
		}
		// An installed copy that fits costs nothing more and is already reviewed.
		if (a.State == "ok") != (b.State == "ok") {
			return a.State == "ok"
		}
		if a.InPack != b.InPack {
			return a.InPack
		}
		if a.AddedTokens != b.AddedTokens {
			return a.AddedTokens < b.AddedTokens
		}
		return a.Name < b.Name
	})
	chosen := map[string]bool{}
	add := func(c Choice) bool {
		if chosen[c.Name] {
			return false
		}
		c.RecSkill = resolve(c.RecSkill)
		if !c.InLibrary {
			return false
		}
		if core.NeedsProjectCopy(c.State) {
			c.AddedTokens = c.Tokens
		}
		chosen[c.Name] = true
		r.Shortlist = append(r.Shortlist, c)
		return true
	}
	if len(words) == 0 {
		// web-quality is suggested indirectly for any frontend.
		covered := map[int]bool{}
		for _, c := range ranked {
			if len(r.Shortlist) >= limit {
				break
			}
			g := r.Groups[c.group]
			if covered[c.group] || g.Reason == "baseline" || g.Pack == "web-quality" {
				continue
			}
			if add(c) {
				covered[c.group] = true
			}
		}
	}
	for _, c := range ranked {
		if len(r.Shortlist) >= limit {
			break
		}
		// Without a task, extra picks in a project with a known stack need
		// evidence beyond pack membership; an empty project gets the baseline.
		if len(words) == 0 && r.HasStack && len(declared) == 0 && c.affinity <= 0 {
			continue
		}
		add(c)
	}
	if brief {
		r.Groups = nil
	}
	if len(r.Shortlist) == 0 {
		r.Message = "No matching local skills. Browse the library, preview a starter pack (core, web-vue, postgres) with skillman bootstrap --pack <pack> --dry-run, or preview an import from a chosen source."
	}
	if !r.HasStack {
		r.Message = strings.TrimSpace("No project stack detected. Pass --stack <id> when known, or preview @core with skillman add -C <project> @core --dry-run. " + r.Message)
	}
	if len(r.Shortlist) > 0 && r.Budget.Total+r.Shortlist[0].AddedTokens > r.Budget.Budget {
		r.Message = strings.TrimSpace(r.Message + " Top choice exceeds the description token budget.")
	}
	// Off-stack choices fill the shortlist for review; install them by default
	// only when nothing in the project's stacks fits the task.
	inStack := false
	for _, c := range r.Shortlist {
		inStack = inStack || !c.offStack
	}
	for _, c := range r.Shortlist {
		if !core.NeedsProjectCopy(c.State) || (inStack && c.offStack) {
			continue
		}
		if len(r.InstallArgs) == 0 {
			r.InstallArgs = []string{"add", "-C", project}
		}
		r.InstallArgs = append(r.InstallArgs, c.Name)
		r.InstallTokens += c.AddedTokens
	}
	topFits := len(r.Shortlist) == 0 || r.Budget.Total+r.Shortlist[0].AddedTokens <= r.Budget.Budget
	if topFits && r.Budget.Total+r.InstallTokens > r.Budget.Budget {
		r.Message = strings.TrimSpace(r.Message + " The suggested install exceeds the description token budget; choose fewer skills.")
	}
	if len(r.InstallArgs) > 0 {
		r.InstallHint = "skillman add -C '" + strings.ReplaceAll(project, "'", "'\"'\"'") + "' " + strings.Join(r.InstallArgs[3:], " ")
	}
	return r
}
