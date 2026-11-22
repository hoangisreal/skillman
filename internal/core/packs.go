package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Pack is a named group of skills; extends pulls in other packs first.
type Pack struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Extends     []string `json:"extends"`
	Skills      []string `json:"skills"`
}

func PackPath(name string) (string, error) {
	if !PACK_NAME.MatchString(name) {
		return "", errf("invalid pack name: %s (kebab-case, optional one 'group/' prefix)", pyRepr(name))
	}
	return filepath.Join(PACKS, name+".yaml"), nil
}

// ListPacks returns every pack name (relative path without .yaml), sorted.
func ListPacks() []string {
	var out []string
	for _, e := range Rglob(PACKS) {
		if strings.HasSuffix(e.Rel, ".yaml") && e.IsFile() {
			out = append(out, strings.TrimSuffix(e.Rel, ".yaml"))
		}
	}
	sort.Strings(out)
	return out
}

func strList(v any) []string {
	xs, _ := v.([]any)
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		out = append(out, pyStr(x))
	}
	return out
}

func LoadPack(name string) (*Pack, error) {
	p, err := PackPath(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, errf("pack not found: %s (see: skill-add --packs)", name)
	}
	v, err := yamlLoad(string(b))
	if err != nil {
		return nil, errf("pack %s: invalid YAML: %s", name, strings.SplitN(err.Error(), "\n", 2)[0])
	}
	data, _ := v.(map[string]any)
	pk := &Pack{Name: name, Extends: strList(data["extends"]), Skills: strList(data["skills"])}
	if d, ok := data["description"]; ok && d != nil && d != "" && d != false {
		pk.Description = pyStr(d)
	}
	return pk, nil
}

func dedup(xs []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// RenderPack produces the YAML text of a pack exactly as PyYAML safe_dump did.
func RenderPack(pk *Pack) string {
	f := []yamlField{{key: "name", str: &pk.Name}, {key: "description", str: &pk.Description}}
	if len(pk.Extends) > 0 {
		f = append(f, yamlField{key: "extends", list: pk.Extends})
	}
	f = append(f, yamlField{key: "skills", list: pk.Skills})
	return dumpYAML(f)
}

// SavePack validates and writes a pack; a save that would create an extends cycle is rolled back.
func SavePack(pk *Pack) error {
	p, err := PackPath(pk.Name)
	if err != nil {
		return err
	}
	skills := dedup(pk.Skills)
	var bad []string
	for _, s := range skills {
		if !KEBAB.MatchString(s) {
			bad = append(bad, s)
		}
	}
	if len(bad) > 0 {
		return errf("invalid skill names: %s", strings.Join(bad, ", "))
	}
	ext := dedup(pk.Extends)
	for _, e := range ext {
		if e == pk.Name {
			return errf("a pack cannot extend itself")
		}
	}
	for _, e := range ext {
		if _, err := PackPath(e); err != nil {
			return err
		}
	}
	doc := &Pack{Name: pk.Name, Description: pk.Description, Extends: ext, Skills: skills}
	unlock, err := DirLock(PACKS)
	if err != nil {
		return err
	}
	defer unlock()
	before, berr := os.ReadFile(p)
	if err := AtomicWrite(p, []byte(RenderPack(doc))); err != nil {
		return err
	}
	if _, err := ExpandPack(pk.Name); err != nil && strings.Contains(err.Error(), "cycle") {
		if berr != nil {
			os.Remove(p)
		} else {
			AtomicWrite(p, before)
		}
		return err
	}
	return nil
}

func DeletePack(name string) (string, error) {
	p, err := PackPath(name)
	if err != nil {
		return "", err
	}
	if !Exists(p) {
		return "", errf("pack not found: %s", name)
	}
	var users []string
	for _, q := range ListPacks() {
		if q == name {
			continue
		}
		if o, err := LoadPack(q); err == nil && contains(o.Extends, name) {
			users = append(users, q)
		}
	}
	if len(users) > 0 {
		return "", errf("pack %s is extended by: %s", name, strings.Join(users, ", "))
	}
	dest, err := trashSlot("pack", strings.ReplaceAll(name, "/", "__"), "")
	if err != nil {
		return "", err
	}
	if err := Move(p, filepath.Join(dest, filepath.Base(p))); err != nil {
		return "", err
	}
	writeTrashMeta(dest, map[string]any{"kind": "pack", "name": name, "origin": p, "removed_at": Now()})
	return dest, nil
}

func RenamePack(oldName, newName string) error {
	po, err := PackPath(oldName)
	if err != nil {
		return err
	}
	pn, err := PackPath(newName)
	if err != nil {
		return err
	}
	if !Exists(po) {
		return errf("pack not found: %s", oldName)
	}
	if Exists(pn) {
		return errf("pack already exists: %s", newName)
	}
	pk, err := LoadPack(oldName)
	if err != nil {
		return err
	}
	pk.Name = newName
	if err := SavePack(pk); err != nil {
		return err
	}
	os.Remove(po)
	for _, q := range ListPacks() {
		o, err := LoadPack(q)
		if err != nil || !contains(o.Extends, oldName) {
			continue
		}
		for i, e := range o.Extends {
			if e == oldName {
				o.Extends[i] = newName
			}
		}
		if err := SavePack(o); err != nil {
			return err
		}
	}
	return nil
}

// ExpandPack resolves extends (depth-first, in order) and dedups.
func ExpandPack(name string) ([]string, error) { return expandPack(name, nil) }

func expandPack(name string, stack []string) ([]string, error) {
	if contains(stack, name) {
		return nil, errf("pack cycle: %s", strings.Join(append(append([]string{}, stack...), name), " -> "))
	}
	pk, err := LoadPack(name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range pk.Extends {
		sub, err := expandPack(e, append(append([]string{}, stack...), name))
		if err != nil {
			return nil, err
		}
		out = append(out, sub...)
	}
	return dedup(append(out, pk.Skills...)), nil
}

// PackProblem is one issue found by ValidatePacks.
type PackProblem struct {
	Pack    string `json:"pack"`
	Problem string `json:"problem"`
}

func ValidatePacks() []PackProblem {
	lib := map[string]bool{}
	for _, n := range LibraryNames() {
		lib[n] = true
	}
	var probs []PackProblem
	for _, n := range ListPacks() {
		pk, err := LoadPack(n)
		if err == nil {
			_, err = ExpandPack(n)
		}
		if err != nil {
			probs = append(probs, PackProblem{n, err.Error()})
			continue
		}
		for _, s := range pk.Skills {
			if !lib[s] {
				probs = append(probs, PackProblem{n, "skill not in library: " + s})
			}
		}
	}
	return probs
}

// PacksBySkill maps skill -> packs that list it directly.
func PacksBySkill() map[string][]string {
	m := map[string][]string{}
	for _, n := range ListPacks() {
		pk, err := LoadPack(n)
		if err != nil {
			continue
		}
		for _, s := range pk.Skills {
			m[s] = append(m[s], n)
		}
	}
	return m
}

// Item is one install request: a skill, optionally attributed to a pack.
type Item struct {
	Name       string
	Pack       string // "" = manual
	OtherPacks []string
	Manual     bool // explicitly requested in addition to a pack
}

// ExpandItems copies each skill once while retaining every reason for installing it.
func ExpandItems(items []string) ([]Item, error) {
	var out []Item
	positions := map[string]int{}
	add := func(n, p string) {
		i, exists := positions[n]
		if !exists {
			positions[n] = len(out)
			out = append(out, Item{Name: n, Pack: p, Manual: p == ""})
			return
		}
		it := &out[i]
		switch {
		case p == "":
			it.Manual = true
		case it.Pack == "":
			it.Pack = p
		case p != it.Pack && !contains(it.OtherPacks, p):
			it.OtherPacks = append(it.OtherPacks, p)
		}
	}
	for _, it := range items {
		if strings.HasPrefix(it, "@") {
			skills, err := ExpandPack(it[1:])
			if err != nil {
				return nil, err
			}
			for _, s := range skills {
				add(s, it[1:])
			}
		} else {
			add(it, "")
		}
	}
	return out, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// PackSummary is a pack with its resolved size (for listings).
type PackSummary struct {
	Pack
	Count int    `json:"count"`
	Error string `json:"error,omitempty"`
}

func PackSummaries(includeVendor bool) []PackSummary {
	var out []PackSummary
	for _, n := range ListPacks() {
		if !includeVendor && strings.HasPrefix(n, "vendor/") {
			continue
		}
		pk, err := LoadPack(n)
		if err != nil {
			out = append(out, PackSummary{Pack: Pack{Name: n}, Error: err.Error()})
			continue
		}
		all, err := ExpandPack(n)
		s := PackSummary{Pack: *pk, Count: len(all)}
		if err != nil {
			s.Error = err.Error()
		}
		out = append(out, s)
	}
	return out
}
