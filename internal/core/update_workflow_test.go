package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLibraryUpdatesPreserveStagingAndPending(t *testing.T) {
	f, _ := newFixture(t, []string{"alpha", "beta", "gamma"}, nil)
	mkskill(t, filepath.Join(f.up, "skills"), "alpha", "New description", "Safe revision.\n", nil)
	for _, name := range []string{"beta", "gamma"} {
		p := filepath.Join(f.up, "skills", name, "SKILL.md")
		text := "---\nname: " + name + "\ndescription: Review example\nhooks:\n  Stop: []\n---\n"
		must(t, os.WriteFile(p, []byte(text), 0644))
	}
	f.push("next")

	const notes = "other-staged-notes.txt"
	must(t, os.WriteFile(filepath.Join(f.lib, notes), []byte("Unrelated staged edit.\n"), 0644))
	sh(t, f.lib, "git", "add", notes)
	assertUnrelatedStillStaged := func() {
		t.Helper()
		if committed := sh(t, f.lib, "git", "show", "--format=", "--name-only", "HEAD"); strings.Contains(committed, notes) {
			t.Fatalf("unrelated notes were committed: %s", committed)
		}
		if staged := sh(t, f.lib, "git", "diff", "--cached", "--name-only"); staged != notes {
			t.Fatalf("unrelated staged paths changed: %q", staged)
		}
	}
	rep := UpdateLibrary([]string{"alpha", "beta"}, "auto-safe", false, true, nil)
	if results := resMap(rep.Results); !results["alpha"].OK || results["beta"].OK {
		t.Fatalf("auto-safe: %+v", rep.Results)
	}
	assertUnrelatedStillStaged()

	UpdateLibrary([]string{"gamma"}, "auto-safe", false, true, nil)
	queued := LoadPending()
	if len(queued) != 2 || queued[0].Name != "beta" || queued[1].Name != "gamma" {
		t.Fatalf("checking a subset dropped unrelated pending updates: %+v", queued)
	}
	rep = UpdateLibrary([]string{"beta"}, "apply", true, true, nil)
	if !resMap(rep.Results)["beta"].OK {
		t.Fatalf("apply: %+v", rep.Results)
	}
	if queued = LoadPending(); len(queued) != 1 || queued[0].Name != "gamma" {
		t.Fatalf("already-applied version is still pending: %+v", queued)
	}
	assertUnrelatedStillStaged()

	must(t, SetLibraryPin("gamma", true))
	results := ApplyUpdates(queued, true, false)
	if len(results) != 1 || results[0].OK || len(LoadPending()) != 0 {
		t.Fatalf("accepted a cached update after its skill was pinned: %+v", results)
	}
	if _, err := Rollback("alpha", true); err != nil {
		t.Fatal(err)
	}
	assertUnrelatedStillStaged()
}
