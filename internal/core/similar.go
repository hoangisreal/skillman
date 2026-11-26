package core

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Similar is another library skill that covers nearly the same ground.
type Similar struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
	Score  int    `json:"score"`  // description word overlap, percent
	Tokens int    `json:"tokens"` // description tokens of the similar skill in the library
}

// WithTokens fills in the library description cost of each similar skill.
func WithTokens(list []Similar) []Similar {
	out := make([]Similar, len(list))
	for i, s := range list {
		s.Tokens = SkillTokens(filepath.Join(SKILLS, s.Name))
		out[i] = s
	}
	return out
}

// SimilarNote says how a similar skill compares on description cost.
func SimilarNote(s Similar, tokens int) string {
	switch d := tokens - s.Tokens; {
	case d > 0:
		return sprintf("similar to %s, which is %d tokens cheaper", s.Name, d)
	case d < 0:
		return sprintf("similar to %s, which costs %d tokens more", s.Name, -d)
	}
	return sprintf("similar to %s, same cost", s.Name)
}

var similarStop = set("this", "that", "with", "when", "your", "from", "into", "only", "used", "uses", "using",
	"should", "must", "skill", "skills", "agent", "agents", "code", "task", "tasks", "requested", "technology",
	"about", "also", "such", "like", "more", "than", "them", "they", "their", "these", "those", "which", "while",
	"what", "will", "work", "working", "help", "helps", "user", "users", "asks", "want", "wants", "make", "need")

func descWords(desc string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(desc), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 4 && !similarStop[w] {
			words[w] = true
		}
	}
	return words
}

func overlap(a, b map[string]bool) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	common := 0
	for w := range a {
		if b[w] {
			common++
		}
	}
	union := len(a) + len(b) - common
	return common * 100 / union
}

// genericSegments add no topic to a skill name ("postgres-best-practices").
var genericSegments = set("best", "practices", "practice", "guide", "guides", "guidelines", "patterns", "pattern",
	"tips", "rules", "skill", "skills", "basics", "fundamentals", "core", "essentials", "expert", "pro",
	"conventions", "standards", "docs", "official", "agent")

// vendorSegments are organisations that publish their own copy of a topic ("vercel-react-best-practices").
var vendorSegments = set("vercel", "anthropic", "openai", "google", "microsoft", "aws", "azure", "cloudflare",
	"supabase", "neon", "github", "sentry", "stripe", "meta", "hashicorp", "mongodb", "redis", "elastic")

// extraSegments returns long's segments outside the contiguous run of short's, or nil if short is not in long.
func extraSegments(long, short string) []string {
	l, s := strings.Split(long, "-"), strings.Split(short, "-")
	for i := 0; i+len(s) <= len(l) && len(s) < len(l); i++ {
		match := true
		for j := range s {
			if l[i+j] != s[j] {
				match = false
				break
			}
		}
		if match {
			return append(append([]string{}, l[:i]...), l[i+len(s):]...)
		}
	}
	return nil
}

// sameTopicName reports whether two names differ only by generic words or a vendor.
func sameTopicName(a, b *IndexRow) bool {
	long, short := a, b
	if len(a.Name) < len(b.Name) {
		long, short = b, a
	}
	extra := extraSegments(long.Name, short.Name)
	if len(extra) == 0 || !slices.ContainsFunc(strings.Split(short.Name, "-"), func(s string) bool { return !genericSegments[s] }) {
		return false
	}
	for _, seg := range extra {
		if !genericSegments[seg] && !vendorSegments[seg] {
			return false
		}
	}
	return true
}

func sharesSegment(a, b string) bool {
	as := strings.Split(a, "-")
	for _, seg := range strings.Split(b, "-") {
		if len(seg) > 2 && !genericSegments[seg] && slices.Contains(as, seg) {
			return true
		}
	}
	return false
}

// similarPair decides whether two skills are near-duplicates. Skills from the
// same source are deliberate siblings (qt-cpp-review, qt-qml-review), never duplicates.
func similarPair(a, b *IndexRow, wa, wb map[string]bool) (int, string, bool) {
	if a.Source == b.Source && strings.Contains(a.Source, "/") {
		return 0, "", false
	}
	score := overlap(wa, wb)
	switch {
	case sameTopicName(a, b):
		return score, "same topic under another name", true
	case score >= 60 && sharesSegment(a.Name, b.Name):
		return score, "nearly the same description", true
	}
	return score, "", false
}

// SimilarSkills finds near-duplicate library skills, keyed by name. Only rows
// whose name is in names are compared (nil compares every row).
func SimilarSkills(rows []IndexRow, names []string) map[string][]Similar {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	words := make([]map[string]bool, len(rows))
	for i := range rows {
		words[i] = descWords(rows[i].Description)
	}
	out := map[string][]Similar{}
	for i := range rows {
		if names != nil && !want[rows[i].Name] {
			continue
		}
		for j := range rows {
			if i == j || (names == nil && j < i) {
				continue
			}
			score, reason, ok := similarPair(&rows[i], &rows[j], words[i], words[j])
			if !ok {
				continue
			}
			out[rows[i].Name] = append(out[rows[i].Name], Similar{Name: rows[j].Name, Reason: reason, Score: score})
			if names == nil {
				out[rows[j].Name] = append(out[rows[j].Name], Similar{Name: rows[i].Name, Reason: reason, Score: score})
			}
		}
	}
	for n := range out {
		sort.Slice(out[n], func(a, b int) bool {
			if out[n][a].Score != out[n][b].Score {
				return out[n][a].Score > out[n][b].Score
			}
			return out[n][a].Name < out[n][b].Name
		})
	}
	return out
}
