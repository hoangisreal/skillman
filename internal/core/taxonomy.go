package core

import (
	_ "embed"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	yaml "go.yaml.in/yaml/v3"
)

//go:embed taxonomy.yaml
var defaultTaxonomy []byte

// DefaultTaxonomyYAML is the built-in taxonomy file (for `skillman taxonomy --default`).
func DefaultTaxonomyYAML() []byte { return defaultTaxonomy }

type Stack struct {
	ID     string   `yaml:"id" json:"id"`
	Title  string   `yaml:"title" json:"title"`
	Packs  []string `yaml:"packs" json:"packs,omitempty"`
	Name   string   `yaml:"name" json:"-"`
	Match  string   `yaml:"match" json:"-"`
	Not    string   `yaml:"not" json:"-"`
	Domain string   `yaml:"-" json:"domain"`
	nameRx *regexp.Regexp
	rx     *regexp.Regexp
	notRx  *regexp.Regexp
}

type Domain struct {
	ID     string   `yaml:"id" json:"id"`
	Title  string   `yaml:"title" json:"title"`
	Stacks []*Stack `yaml:"stacks" json:"stacks"`
}

type SkillOverride struct {
	Domain string   `yaml:"domain"`
	Stacks []string `yaml:"stacks"`
}

type Taxonomy struct {
	Domains    []*Domain                `yaml:"domains"`
	TagDomains map[string]string        `yaml:"tag_domains"`
	Skills     map[string]SkillOverride `yaml:"skills"`
	Errors     []string                 `yaml:"-"`
	stacks     map[string]*Stack
	domains    map[string]*Domain
	byPack     map[string][]*Stack
}

const GeneralDomain = "general"

var taxCache struct {
	sync.Mutex
	key string
	t   *Taxonomy
}

func compileRx(s string, errs *[]string, where string) *regexp.Regexp {
	if s == "" {
		return nil
	}
	r, err := regexp.Compile(s)
	if err != nil {
		*errs = append(*errs, where+": "+err.Error())
		return nil
	}
	return r
}

// LoadTaxonomy returns the built-in taxonomy merged with $SKILL_LIBRARY/taxonomy.yaml (cached by mtime).
func LoadTaxonomy() *Taxonomy {
	over := filepath.Join(LIB, "taxonomy.yaml")
	key := over
	if st, err := os.Stat(over); err == nil {
		key += st.ModTime().String()
	}
	taxCache.Lock()
	defer taxCache.Unlock()
	if taxCache.t != nil && taxCache.key == key {
		return taxCache.t
	}
	t := &Taxonomy{}
	if err := yaml.Unmarshal(defaultTaxonomy, t); err != nil {
		panic("built-in taxonomy: " + err.Error())
	}
	if b, err := os.ReadFile(over); err == nil {
		var o Taxonomy
		if err := yaml.Unmarshal(b, &o); err != nil {
			t.Errors = append(t.Errors, "taxonomy.yaml: "+strings.SplitN(err.Error(), "\n", 2)[0])
		} else {
			for _, od := range o.Domains {
				replaced := false
				for i, d := range t.Domains {
					if d.ID == od.ID {
						t.Domains[i], replaced = od, true
					}
				}
				if !replaced {
					t.Domains = append(t.Domains, od)
				}
			}
			for k, v := range o.TagDomains {
				t.TagDomains[k] = v
			}
			if t.Skills == nil {
				t.Skills = map[string]SkillOverride{}
			}
			for k, v := range o.Skills {
				t.Skills[k] = v
			}
		}
	}
	t.index()
	taxCache.key, taxCache.t = key, t
	return t
}

func (t *Taxonomy) index() {
	t.stacks, t.domains, t.byPack = map[string]*Stack{}, map[string]*Domain{}, map[string][]*Stack{}
	for _, d := range t.Domains {
		t.domains[d.ID] = d
		for _, s := range d.Stacks {
			s.Domain = d.ID
			where := "stack " + s.ID
			s.nameRx = compileRx(s.Name, &t.Errors, where+" name")
			s.rx = compileRx(s.Match, &t.Errors, where+" match")
			s.notRx = compileRx(s.Not, &t.Errors, where+" not")
			if _, dup := t.stacks[s.ID]; dup {
				t.Errors = append(t.Errors, "duplicate stack id: "+s.ID)
			}
			t.stacks[s.ID] = s
			for _, p := range s.Packs {
				t.byPack[p] = append(t.byPack[p], s)
			}
		}
	}
}

func (t *Taxonomy) Stack(id string) *Stack   { return t.stacks[id] }
func (t *Taxonomy) Domain(id string) *Domain { return t.domains[id] }

// StacksForPack lists the stacks a pack belongs to (also through composite packs' extends).
func (t *Taxonomy) StacksForPack(pack string) []*Stack {
	out := append([]*Stack{}, t.byPack[pack]...)
	if len(out) == 0 {
		if pk, err := LoadPack(pack); err == nil {
			for _, e := range pk.Extends {
				out = append(out, t.StacksForPack(e)...)
			}
		}
	}
	return out
}

// DomainTitle returns a domain's display title ("General" for the fallback domain).
func (t *Taxonomy) DomainTitle(id string) string {
	if d := t.domains[id]; d != nil {
		return d.Title
	}
	if id == GeneralDomain {
		return "General / other"
	}
	return id
}

type stackScore struct {
	s     *Stack
	score int
	order int
}

const maxStacks = 4

// Classify sets row.Domain and row.Stacks from overrides, packs, name and description.
func (t *Taxonomy) Classify(row *IndexRow, packs []string) {
	if o, ok := t.Skills[row.Name]; ok && (o.Domain != "" || len(o.Stacks) > 0) {
		row.Stacks = append([]string{}, o.Stacks...)
		row.Domain = o.Domain
		if row.Domain == "" && len(o.Stacks) > 0 {
			if s := t.stacks[o.Stacks[0]]; s != nil {
				row.Domain = s.Domain
			}
		}
		if row.Domain == "" {
			row.Domain = GeneralDomain
		}
		return
	}
	nameHay := strings.ToLower(strings.ReplaceAll(row.Name, "-", " "))
	descHay := strings.ToLower(row.Description)
	var scored []stackScore
	order := 0
	for _, d := range t.Domains {
		for _, s := range d.Stacks {
			order++
			sc := 0
			for _, p := range packs {
				if contains(s.Packs, p) {
					sc += 8
					break
				}
			}
			vetoed := s.notRx != nil && (s.notRx.MatchString(nameHay) || s.notRx.MatchString(descHay))
			if !vetoed {
				if s.nameRx != nil && s.nameRx.MatchString(row.Name) {
					sc += 4
				} else if s.rx != nil && s.rx.MatchString(nameHay) {
					sc += 3
				}
				if s.rx != nil && s.rx.MatchString(descHay) {
					sc++
				}
			}
			if sc > 0 {
				scored = append(scored, stackScore{s, sc, order})
			}
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].order < scored[j].order
	})
	row.Stacks = nil
	for i, s := range scored {
		// description-only hits are weak: keep them only if nothing stronger exists, and keep few
		if i >= maxStacks || (s.score == 1 && scored[0].score > 1) {
			break
		}
		row.Stacks = append(row.Stacks, s.s.ID)
	}
	switch {
	case len(scored) > 0:
		row.Domain = scored[0].s.Domain
	default:
		row.Domain = GeneralDomain
		for _, tag := range row.Tags {
			if d, ok := t.TagDomains[tag]; ok {
				row.Domain = d
				break
			}
		}
	}
}

// Domains of a classified row: the primary one first, then the domains of its other stacks.
func (t *Taxonomy) RowDomains(row *IndexRow) []string {
	out := []string{row.Domain}
	for _, id := range row.Stacks {
		if s := t.stacks[id]; s != nil && !contains(out, s.Domain) {
			out = append(out, s.Domain)
		}
	}
	return out
}
