// Package search is the in-memory skill search used by the TUI and `skillman search`.
// Everything that can be precomputed (lower-cased haystacks, pack/stack membership) is built once,
// so a query over ~1000 skills costs well under a millisecond.
package search

import (
	"sort"
	"strings"

	"github.com/hoangisreal/skillman/internal/core"
)

type Doc struct {
	Row        core.IndexRow
	Packs      []string // every pack listing the skill (incl. vendor/*)
	StackPacks []string // non-vendor packs
	Domains    []string // primary first
	State      string   // project state ("" = not in project)
	name, src  string
	desc, hay  string
}

type Index struct {
	Docs []Doc
	pos  map[string]int
}

// Build indexes rows; packs maps skill -> packs, state maps skill -> project state.
func Build(rows []core.IndexRow, packs map[string][]string, state map[string]string, tax *core.Taxonomy) *Index {
	ix := &Index{Docs: make([]Doc, len(rows)), pos: make(map[string]int, len(rows))}
	for i, r := range rows {
		d := Doc{Row: r, Packs: packs[r.Name], State: state[r.Name]}
		for _, p := range d.Packs {
			if !strings.HasPrefix(p, "vendor/") {
				d.StackPacks = append(d.StackPacks, p)
			}
		}
		if tax != nil {
			d.Domains = tax.RowDomains(&d.Row)
		} else {
			d.Domains = []string{r.Domain}
		}
		d.name = strings.ToLower(r.Name)
		d.src = strings.ToLower(r.Source)
		d.desc = strings.ToLower(r.Description)
		d.hay = d.name + " " + d.src + " " + d.desc
		ix.Docs[i] = d
		ix.pos[r.Name] = i
	}
	return ix
}

// SetState updates project states in place (after install/remove) without rebuilding.
func (ix *Index) SetState(state map[string]string) {
	for i := range ix.Docs {
		ix.Docs[i].State = state[ix.Docs[i].Row.Name]
	}
}

func (ix *Index) Get(name string) *Doc {
	if i, ok := ix.pos[name]; ok {
		return &ix.Docs[i]
	}
	return nil
}

// Query is a parsed search string.
type Query struct {
	Words, Not                         []string
	Tags, Srcs, Packs, Domains, Stacks []string
	NoPack, Installed, NotInstalled    bool
}

// Parse understands: words, -word, tag:x, src:x, pack:x, domain:x, stack:x, nopack, installed, notinstalled.
func Parse(q string) Query {
	var out Query
	for _, w := range strings.Fields(strings.ToLower(q)) {
		k, v, hasColon := strings.Cut(w, ":")
		switch {
		case hasColon && v != "" && k == "tag":
			out.Tags = append(out.Tags, v)
		case hasColon && v != "" && k == "src":
			out.Srcs = append(out.Srcs, v)
		case hasColon && v != "" && k == "pack":
			out.Packs = append(out.Packs, strings.TrimPrefix(v, "@"))
		case hasColon && v != "" && (k == "domain" || k == "d"):
			out.Domains = append(out.Domains, v)
		case hasColon && v != "" && (k == "stack" || k == "s"):
			out.Stacks = append(out.Stacks, v)
		case w == "nopack":
			out.NoPack = true
		case w == "installed":
			out.Installed = true
		case w == "notinstalled" || w == "!installed":
			out.NotInstalled = true
		case strings.HasPrefix(w, "-") && len(w) > 1:
			out.Not = append(out.Not, w[1:])
		default:
			out.Words = append(out.Words, w)
		}
	}
	return out
}

func (q Query) Empty() bool {
	return len(q.Words)+len(q.Not)+len(q.Tags)+len(q.Srcs)+len(q.Packs)+len(q.Domains)+len(q.Stacks) == 0 &&
		!q.NoPack && !q.Installed && !q.NotInstalled
}

func anyHas(xs []string, f func(string) bool) bool {
	for _, x := range xs {
		if f(x) {
			return true
		}
	}
	return false
}

func (d *Doc) matchFilters(q *Query) bool {
	for _, t := range q.Tags {
		if !anyHas(d.Row.Tags, func(x string) bool { return strings.Contains(x, t) }) {
			return false
		}
	}
	for _, s := range q.Srcs {
		if !strings.Contains(d.src, s) {
			return false
		}
	}
	for _, p := range q.Packs {
		if !anyHas(d.Packs, func(x string) bool { return x == p || x[strings.LastIndex(x, "/")+1:] == p }) {
			return false
		}
	}
	for _, dm := range q.Domains {
		if !anyHas(d.Domains, func(x string) bool { return strings.HasPrefix(x, dm) }) {
			return false
		}
	}
	for _, s := range q.Stacks {
		if !anyHas(d.Row.Stacks, func(x string) bool { return x == s || strings.HasPrefix(x, s) }) {
			return false
		}
	}
	if q.NoPack && len(d.StackPacks) > 0 {
		return false
	}
	if q.Installed && d.State == "" {
		return false
	}
	if q.NotInstalled && d.State != "" {
		return false
	}
	for _, w := range q.Not {
		if strings.Contains(d.hay, w) {
			return false
		}
	}
	return true
}

func wordBoundary(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		if j == 0 || !isWordByte(s[j-1]) {
			return true
		}
		i = j + 1
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_'
}

// score ranks a doc for the plain words (0 = no match). Every word must match somewhere.
func (d *Doc) score(words []string) int {
	total := 0
	for _, w := range words {
		s := 0
		switch {
		case d.name == w:
			s = 1000
		case strings.HasPrefix(d.name, w):
			s = 400
		case strings.Contains(d.name, "-"+w):
			s = 250
		case strings.Contains(d.name, w):
			s = 150
		case wordBoundary(d.desc, w):
			s = 40
		case strings.Contains(d.desc, w):
			s = 15
		case strings.Contains(d.src, w):
			s = 5
		default:
			return 0
		}
		total += s
	}
	return total
}

// Search returns doc indices matching q, best first (library order for filter-only queries).
// within limits the candidates (nil = all docs).
func (ix *Index) Search(q Query, within []int) []int {
	type hit struct{ i, s int }
	hits := make([]hit, 0, 64)
	visit := func(i int) {
		d := &ix.Docs[i]
		if !d.matchFilters(&q) {
			return
		}
		s := 1
		if len(q.Words) > 0 {
			if s = d.score(q.Words); s == 0 {
				return
			}
		}
		hits = append(hits, hit{i, s})
	}
	if within != nil {
		for _, i := range within {
			visit(i)
		}
	} else {
		for i := range ix.Docs {
			visit(i)
		}
	}
	if len(q.Words) > 0 {
		sort.SliceStable(hits, func(a, b int) bool { return hits[a].s > hits[b].s })
	}
	out := make([]int, len(hits))
	for k, h := range hits {
		out[k] = h.i
	}
	return out
}

// Names is a convenience for CLI output.
func (ix *Index) Names(idx []int) []string {
	out := make([]string, len(idx))
	for k, i := range idx {
		out[k] = ix.Docs[i].Row.Name
	}
	return out
}

// Pos returns the doc index of a skill.
func (ix *Index) Pos(name string) (int, bool) {
	i, ok := ix.pos[name]
	return i, ok
}
