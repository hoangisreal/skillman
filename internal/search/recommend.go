package search

import (
	"sort"
	"strings"

	"github.com/hoangisreal/skillman/internal/core"
)

type Choice struct {
	core.RecSkill
	Reason      string   `json:"reason"`
	Evidence    string   `json:"evidence"`
	AddedTokens int      `json:"added_tokens"`
	Matched     []string `json:"matched_keywords"`
	score       int
	group       int
}

type Recommendation struct {
	core.Recommendation
	Shortlist   []Choice `json:"shortlist"`
	InstallArgs []string `json:"install_args"` // arguments after the executable
	InstallHint string   `json:"install_hint"`
	Message     string   `json:"message,omitempty"`
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

func Rank(catalog *core.RecommendationCatalog, project string, rows []core.IndexRow, task string, limit, extra int, brief bool) Recommendation {
	r := Recommendation{Recommendation: catalog.Candidates(project, rows, -1), Shortlist: []Choice{}, InstallArgs: []string{}}
	words := strings.Fields(strings.ToLower(task))
	var ranked []Choice
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
	for i := range r.Groups {
		g := &r.Groups[i]
		visible := g.Skills[:0]
		extras := 0
		for _, s := range g.Skills {
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
			if s.InPack {
				c.Reason = "@" + g.Pack
			}
			if len(words) > 0 {
				doc := Doc{name: s.Name, desc: strings.ToLower(s.Description), src: strings.ToLower(s.Source)}
				score, matched := taskMatch(doc, words)
				if score == 0 {
					continue
				}
				c.score, c.Matched = score, matched
				c.Reason += " · " + strings.Join(matched, ", ")
			}
			ranked = append(ranked, c)
		}
		g.Skills = visible
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.InPack != b.InPack {
			return a.InPack
		}
		if (a.Evidence == "baseline") != (b.Evidence == "baseline") {
			return b.Evidence == "baseline"
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
		add(c)
	}
	if brief {
		r.Groups = nil
	}
	if len(r.Shortlist) == 0 {
		r.Message = "No matching local skills. Browse the library or preview a starter pack with skillman bootstrap --pack <pack> --dry-run."
	}
	for _, c := range r.Shortlist {
		if !core.NeedsProjectCopy(c.State) {
			continue
		}
		if len(r.InstallArgs) == 0 {
			r.InstallArgs = []string{"add", "-C", project}
		}
		r.InstallArgs = append(r.InstallArgs, c.Name)
	}
	if len(r.InstallArgs) > 0 {
		r.InstallHint = "skillman add -C '" + strings.ReplaceAll(project, "'", "'\"'\"'") + "' " + strings.Join(r.InstallArgs[3:], " ")
	}
	return r
}
