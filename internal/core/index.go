package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LibraryNames lists library skills (dirs with SKILL.md), sorted.
func LibraryNames() []string {
	var out []string
	for _, e := range SortedDir(SKILLS) {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		p := filepath.Join(SKILLS, n)
		if IsDir(p) && Exists(filepath.Join(p, "SKILL.md")) {
			out = append(out, n)
		}
	}
	return out
}

// Finding is one validation message for a skill.
type Finding struct {
	Name, Msg string
}

// RebuildIndex validates every skill and writes INDEX.json/INDEX.md (when writeIndex) and VALIDATION.md.
func RebuildIndex(writeIndex bool) ([]IndexRow, []Finding, []Finding, error) {
	var errs, warns []Finding
	var rows []IndexRow
	pbs := PacksBySkill()
	tax := LoadTaxonomy()
	for _, e := range SortedDir(SKILLS) {
		n := e.Name()
		p := filepath.Join(SKILLS, n)
		if strings.HasPrefix(n, ".") || !IsDir(p) {
			continue
		}
		es, ws, row := CheckSkill(p, n)
		for _, m := range es {
			errs = append(errs, Finding{n, m})
		}
		for _, m := range ws {
			warns = append(warns, Finding{n, m})
		}
		if row != nil {
			rows = append(rows, *row)
		}
	}
	if rows == nil {
		rows = []IndexRow{}
	}
	ClassifyRows(rows, tax, pbs)
	if writeIndex {
		b, err := MarshalPy(rows, 1)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := AtomicWrite(filepath.Join(LIB, "INDEX.json"), b); err != nil {
			return nil, nil, nil, err
		}
		out := []string{"# Skill Library Index", "",
			fmt.Sprintf("%d skills. Search: `skill-search <query>`. Add: `skill-add <name> [dir]`.", len(rows)), "",
			"| skill | tags | description | source | lines |", "|---|---|---|---|---|"}
		for _, r := range rows {
			d := strings.ReplaceAll(r.Description, "|", "\\|")
			out = append(out, fmt.Sprintf("| %s | %s | %s | %s | %d |", r.Name, strings.Join(r.Tags, ","), d, r.Source, r.Lines))
		}
		if err := AtomicWrite(filepath.Join(LIB, "INDEX.md"), []byte(strings.Join(out, "\n")+"\n")); err != nil {
			return nil, nil, nil, err
		}
	}
	rep := []string{"# Validation report", "", fmt.Sprintf("skills: %d  errors: %d  warnings: %d", len(rows), len(errs), len(warns)), ""}
	for _, f := range errs {
		rep = append(rep, fmt.Sprintf("- ERROR `%s`: %s", f.Name, f.Msg))
	}
	for _, f := range warns {
		rep = append(rep, fmt.Sprintf("- WARN `%s`: %s", f.Name, f.Msg))
	}
	if err := AtomicWrite(filepath.Join(LIB, "VALIDATION.md"), []byte(strings.Join(rep, "\n")+"\n")); err != nil {
		return nil, nil, nil, err
	}
	return rows, errs, warns, nil
}

// LoadIndex reads INDEX.json and (re)classifies every row with the current taxonomy and packs, so taxonomy
// edits and pack changes show up without rebuilding the index.
func LoadIndex() []IndexRow {
	var rows []IndexRow
	if !LoadJSON(filepath.Join(LIB, "INDEX.json"), &rows) {
		return nil
	}
	ClassifyRows(rows, LoadTaxonomy(), PacksBySkill())
	return rows
}

// classification cache: regex classification of ~700 skills costs ~0.5 s, so results are kept in
// .cache/classify.json keyed by a signature of the taxonomy and every pack's skill list.
type classEntry struct {
	Desc   string   `json:"desc"`
	Tags   []string `json:"tags"`
	Domain string   `json:"domain"`
	Stacks []string `json:"stacks"`
}

type classCache struct {
	Sig  string                `json:"sig"`
	Rows map[string]classEntry `json:"rows"`
}

func classSignature(tax *Taxonomy, pbs map[string][]string) string {
	h := sha256.New()
	h.Write(defaultTaxonomy)
	if b, err := os.ReadFile(filepath.Join(LIB, "taxonomy.yaml")); err == nil {
		h.Write(b)
	}
	names := make([]string, 0, len(pbs))
	for n := range pbs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h.Write([]byte(n + "=" + strings.Join(pbs[n], ",") + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// ClassifyRows sets domain/stacks on every row, reusing cached results when nothing relevant changed.
func ClassifyRows(rows []IndexRow, tax *Taxonomy, pbs map[string][]string) {
	sig := classSignature(tax, pbs)
	cp := filepath.Join(CACHE, "classify.json")
	var cc classCache
	LoadJSON(cp, &cc)
	if cc.Sig != sig || cc.Rows == nil {
		cc = classCache{Sig: sig, Rows: map[string]classEntry{}}
	}
	dirty := false
	for i := range rows {
		r := &rows[i]
		if e, ok := cc.Rows[r.Name]; ok && e.Desc == r.Description && strings.Join(e.Tags, ",") == strings.Join(r.Tags, ",") {
			r.Domain, r.Stacks = e.Domain, e.Stacks
			continue
		}
		tax.Classify(r, pbs[r.Name])
		cc.Rows[r.Name] = classEntry{r.Description, r.Tags, r.Domain, r.Stacks}
		dirty = true
	}
	if dirty && IsDir(LIB) {
		if b, err := MarshalPy(cc, 0); err == nil {
			AtomicWrite(cp, b) // best effort (read-only library is fine)
		}
	}
}

// IndexMTime is used by long-running UIs to notice an index rebuilt by another process.
func IndexMTime() int64 {
	st, err := os.Stat(filepath.Join(LIB, "INDEX.json"))
	if err != nil {
		return 0
	}
	return st.ModTime().UnixNano()
}
