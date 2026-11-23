package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Golden outputs produced by the original Python skillcore.py (testdata/gen_compat.py).
type compatExpected struct {
	TreeHash map[string]string `json:"tree_hash"`
	Check    map[string]struct {
		Errors []string        `json:"errors"`
		Warns  []string        `json:"warns"`
		Row    json.RawMessage `json:"row"`
	} `json:"check"`
	Tokens   map[string]int `json:"tokens"`
	PackYAML []struct {
		Pack Pack   `json:"pack"`
		YAML string `json:"yaml"`
	} `json:"pack_yaml"`
	Diff map[string]string `json:"diff"`
}

func loadCompat(t *testing.T) (compatExpected, string) {
	t.Helper()
	base := filepath.Join("testdata", "compat")
	b, err := os.ReadFile(filepath.Join(base, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e compatExpected
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e, base
}

func TestCompatTreeHash(t *testing.T) {
	e, base := loadCompat(t)
	for name, want := range e.TreeHash {
		if runtime.GOOS == "windows" && name == "links" {
			// Windows reports native backslashes in the link targets. Keep the
			// existing hash contract for manifests written on that platform.
			want = "f090468d9a5cbadb"
		}
		if got := TreeHash(filepath.Join(base, "lib", "skills", name)); got != want {
			t.Errorf("tree_hash(%s) = %s, python %s", name, got, want)
		}
	}
}

func TestCompatCheckSkill(t *testing.T) {
	e, base := loadCompat(t)
	for name, want := range e.Check {
		errs, warns, row := CheckSkill(filepath.Join(base, "lib", "skills", name), name)
		if !reflect.DeepEqual(yamlMsg(nz(errs)), yamlMsg(nz(want.Errors))) {
			t.Errorf("%s errors:\n go %q\n py %q", name, errs, want.Errors)
		}
		if !reflect.DeepEqual(nz(warns), nz(want.Warns)) {
			t.Errorf("%s warns:\n go %q\n py %q", name, warns, want.Warns)
		}
		if string(want.Row) == "null" {
			if row != nil {
				t.Errorf("%s: row should be nil", name)
			}
			continue
		}
		var py IndexRow
		json.Unmarshal(want.Row, &py)
		row.Source = ""
		if !reflect.DeepEqual(*row, py) {
			t.Errorf("%s row:\n go %+v\n py %+v", name, *row, py)
		}
		if got := SkillTokens(filepath.Join(base, "lib", "skills", name)); got != e.Tokens[name] {
			t.Errorf("%s tokens go %d py %d", name, got, e.Tokens[name])
		}
	}
}

func TestCompatPackYAML(t *testing.T) {
	e, _ := loadCompat(t)
	for _, c := range e.PackYAML {
		pk := c.Pack
		if got := RenderPack(&pk); got != c.YAML {
			t.Errorf("pack yaml for description %q:\n go %q\n py %q", pk.Description, got, c.YAML)
		}
	}
}

func TestCompatDiff(t *testing.T) {
	e, base := loadCompat(t)
	a, b := filepath.Join(base, "diff", "a"), filepath.Join(base, "diff", "b")
	cases := map[string]string{
		"ab":     DiffText(a, b, "library", "project", 0),
		"labels": DiffText(a, b, "current", "upstream", 0),
		"same":   DiffText(filepath.Join(a, "x"), filepath.Join(a, "y"), "library", "project", 0),
	}
	for k, got := range cases {
		if got != e.Diff[k] {
			t.Errorf("diff %s:\n go %q\n py %q", k, got, e.Diff[k])
		}
	}
}

// yamlMsg drops parser-specific detail after "invalid YAML:" (PyYAML and go-yaml word errors differently).
func yamlMsg(xs []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		if len(x) > 13 && x[:13] == "invalid YAML:" {
			x = "invalid YAML:"
		}
		out[i] = x
	}
	return out
}

// TestCompatRealLibrary compares against Python output for a real library when
// SKILLMAN_COMPAT_LIB (a library copy) and SKILLMAN_COMPAT_HASHES (py_oracle.py output) are set.
func TestCompatRealLibrary(t *testing.T) {
	lib, hp := os.Getenv("SKILLMAN_COMPAT_LIB"), os.Getenv("SKILLMAN_COMPAT_HASHES")
	if lib == "" || hp == "" {
		t.Skip("set SKILLMAN_COMPAT_LIB and SKILLMAN_COMPAT_HASHES")
	}
	t.Setenv("SKILL_LIBRARY", lib)
	Configure()
	var py struct {
		TreeHash map[string]string `json:"tree_hash"`
		Tokens   map[string]int    `json:"tokens"`
	}
	b, _ := os.ReadFile(hp)
	json.Unmarshal(b, &py)
	bad := 0
	for _, n := range LibraryNames() {
		if got := TreeHash(filepath.Join(SKILLS, n)); got != py.TreeHash[n] {
			bad++
			t.Errorf("tree_hash %s go %s py %s", n, got, py.TreeHash[n])
		}
		if got := SkillTokens(filepath.Join(SKILLS, n)); got != py.Tokens[n] {
			t.Errorf("tokens %s go %d py %d", n, got, py.Tokens[n])
		}
	}
	if len(py.TreeHash) != len(LibraryNames()) {
		t.Errorf("skill count go %d py %d", len(LibraryNames()), len(py.TreeHash))
	}
	if _, _, _, err := RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d skills compared, %d hash mismatches", len(LibraryNames()), bad)
}
