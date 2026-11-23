package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
)

func TestUpstreamQueue(t *testing.T) {
	lib, _ := setup(t)
	base := t.TempDir()
	repo := filepath.Join(base, "example", "skills.git")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLCORE_GIT_BASE", filepath.ToSlash(base)+"/")
	core.Configure()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.invalid"}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	write := func(p, content string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git(repo, "init", "-q", "-b", "main")
	write(filepath.Join(repo, "LICENSE"), "MIT\n")
	mkskill(t, repo, "one", "Review the first example")
	mkskill(t, repo, "two", "Review the second example")
	mkskill(t, repo, "invalid", "")
	mkskill(t, repo, "review", "Review an example with hook metadata")
	write(filepath.Join(repo, "skills", "review", "SKILL.md"), "---\nname: review\ndescription: Review hook metadata\nhooks:\n  Stop: []\n---\n")
	git(repo, "add", ".")
	git(repo, "commit", "-qm", "Initial source")
	write(filepath.Join(lib, "staging", "repos.txt"), "official core example/skills\n")
	runOK(t, "pack", "create", "qa-one", "one")
	runOK(t, "pack", "create", "qa-two", "two")

	candidates := func(flags ...string) []core.Candidate {
		t.Helper()
		var report struct{ Candidates []core.Candidate }
		if rc := runJSON(t, &report, append([]string{"upstream", "candidates"}, flags...)...); rc != 0 {
			t.Fatalf("candidates: rc=%d", rc)
		}
		return report.Candidates
	}
	preview := func(wantRC int, flags ...string) []core.ImportLine {
		t.Helper()
		var report struct{ Results []core.ImportLine }
		if rc := runJSON(t, &report, append([]string{"upstream", "import"}, flags...)...); rc != wantRC {
			t.Fatalf("import %v: rc=%d, %+v", flags, rc, report)
		}
		return report.Results
	}
	tick := func(name string) {
		t.Helper()
		for _, c := range candidates() {
			if c.Name == name {
				runOK(t, "upstream", "tick", "--line", strconv.Itoa(c.Line))
				return
			}
		}
		t.Fatalf("missing candidate %q", name)
	}

	runOK(t, "upstream", "verify")
	if results := preview(0, "--dry-run"); results == nil || len(results) != 0 {
		t.Fatalf("empty imports must return an empty JSON array: %+v", results)
	}
	if rc, _, _ := run(t, "upstream", "tick", "--line", "1"); rc != 1 {
		t.Fatalf("invalid candidate line: rc=%d", rc)
	}
	runOK(t, "upstream", "tick", "--pack", "qa-one", "qa-two")
	runOK(t, "upstream", "verify")
	if rows := candidates("--ticked"); len(rows) != 2 {
		t.Fatalf("re-verifying an unchanged repo lost or omitted ticks: %+v", rows)
	}
	runOK(t, "upstream", "tick", "--reset", "--pack", "qa-one", "--repo", "other/skills")
	if rows := candidates("--ticked"); len(rows) != 0 {
		t.Fatalf("repo filter selected unrelated candidates: %+v", rows)
	}

	tick("invalid")
	if results := preview(1, "--dry-run"); len(results) != 1 || !strings.Contains(results[0].Detail, "missing description") {
		t.Fatalf("validation in preview: %+v", results)
	}
	if results := preview(1, "--allow-risky"); len(results) != 1 || results[0].Result != "FAILED" {
		t.Fatalf("validation errors must block apply: %+v", results)
	}
	if core.Lexists(filepath.Join(lib, "skills", "invalid")) {
		t.Fatal("invalid skill was installed")
	}
	runOK(t, "upstream", "tick", "--reset", "--repo", "other/skills")
	tick("review")
	if results := preview(1, "--dry-run"); len(results) != 1 || !strings.Contains(results[0].Detail, "audit review") {
		t.Fatalf("review was not required: %+v", results)
	}
	if results := preview(0, "--dry-run", "--allow-risky"); len(results) != 1 || results[0].Result != "DRY-RUN" {
		t.Fatalf("allowed preview: %+v", results)
	}

	runOK(t, "upstream", "tick", "--reset", "--pack", "qa-one")
	log := filepath.Join(lib, "IMPORT_LOG.md")
	// Changes in the clone cache are permitted during a preview.
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if results := preview(0, "--dry-run"); len(results) != 1 || results[0].Result != "DRY-RUN" {
		t.Fatalf("preview: %+v", results)
	}
	after, err := os.ReadFile(log)
	if err != nil || string(after) != string(before) || core.Lexists(filepath.Join(lib, "skills", "one")) {
		t.Fatalf("preview changed the log or installed a skill: %v", err)
	}
	if results := preview(0); len(results) != 1 || results[0].Result != "IMPORTED" {
		t.Fatalf("apply: %+v", results)
	}
	git(lib, "add", ".")
	git(lib, "commit", "-qm", "Baseline library")
	mkskill(t, repo, "one", "Updated first example")
	git(repo, "add", ".")
	git(repo, "commit", "-qm", "Source update")
	runOK(t, "upstream", "verify")
	if rows := candidates("--ticked"); len(rows) != 0 {
		t.Fatalf("a different commit requires a fresh selection: %+v", rows)
	}
	tick("one")
	local := filepath.Join(lib, "skills", "one", "SKILL.md")
	write(local, "Local edit to keep")
	if results := preview(1, "--update"); len(results) != 1 || !strings.Contains(results[0].Detail, "uncommitted changes") {
		t.Fatalf("dirty library update: %+v", results)
	}
	body, err := os.ReadFile(local)
	if err != nil || string(body) != "Local edit to keep" {
		t.Fatal("bulk update overwrote a local edit", err)
	}
	var report core.UpdateReport
	if rc := runJSON(t, &report, "library", "apply", "one"); rc != 1 || len(report.Results) != 1 || report.Results[0].OK {
		t.Fatalf("JSON update report hid a blocked update: rc=%d %+v", rc, report.Results)
	}
}
