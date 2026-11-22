package search

import (
	"sort"
	"strings"

	"github.com/hoangisreal/skillman/internal/core"
)

type Choice struct {
	core.RecSkill
	Reason      string `json:"reason"`
	Evidence    string `json:"evidence"`
	AddedTokens int    `json:"added_tokens"`
	score       int
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
	r := Recommendation{Recommendation: core.Recommend(project, rows, -1), Shortlist: []Choice{}, InstallArgs: []string{}}
	best := map[string]Choice{}
	words := strings.Fields(strings.ToLower(task))
	for i := range r.Groups {
		g := &r.Groups[i]
		visible := g.Skills[:0]
		extras := 0
		for _, s := range g.Skills {
			if s.InPack || extra < 0 || extras < extra {
				visible = append(visible, s)
				if !s.InPack {
					extras++
				}
			}
			if !s.InLibrary {
				continue
			}
			c := Choice{RecSkill: s, Evidence: g.Reason, Reason: strings.Join(g.Stacks, ", "), AddedTokens: s.Tokens}
			if !core.NeedsProjectCopy(s.State) {
				c.AddedTokens = 0
			}
			if s.InPack {
				c.Reason = "@" + g.Pack
				c.score += 2
			}
			if g.Reason != "baseline" {
				c.score += 4
			}
			doc := Doc{name: s.Name, desc: strings.ToLower(s.Description), src: strings.ToLower(s.Source)}
			if len(words) > 0 {
				ts, matched := taskMatch(doc, words)
				if ts == 0 {
					continue
				}
				c.score += ts
				c.Reason += " · " + strings.Join(matched, ", ")
			}
			if prev, ok := best[s.Name]; !ok || c.score > prev.score {
				best[s.Name] = c
			}
		}
		g.Skills = visible
	}
	for _, c := range best {
		r.Shortlist = append(r.Shortlist, c)
	}
	sort.Slice(r.Shortlist, func(i, j int) bool {
		a, b := r.Shortlist[i], r.Shortlist[j]
		if a.score != b.score {
			return a.score > b.score
		}
		return a.Name < b.Name
	})
	if len(r.Shortlist) > limit {
		r.Shortlist = r.Shortlist[:limit]
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
