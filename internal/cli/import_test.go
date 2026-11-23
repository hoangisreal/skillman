package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
)

func TestImportWorkflow(t *testing.T) {
	lib, p := setup(t)
	p, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	gitBase := filepath.Join(tmp, "upstreams")
	repo := filepath.Join(gitBase, "acme", "skills.git")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILLCORE_GIT_BASE", filepath.ToSlash(gitBase)+"/")
	core.Configure()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=test", "-c", "user.email=test@example.invalid"}, args...)...)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", args, b, err)
		}
		return strings.TrimSpace(string(b))
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	mkskill(t, repo, "example-skill", "Review example inputs")
	mkskill(t, repo, "second-skill", "Use on another example")
	git("add", ".")
	git("commit", "-qm", "first")
	invoke := func(want int, args ...string) importReport {
		t.Helper()
		var report importReport
		if rc := runJSON(t, &report, append([]string{"import"}, args...)...); rc != want {
			t.Fatalf("%v: rc=%d, report=%+v", args, rc, report)
		}
		return report
	}
	const slug = "acme/skills"
	selected := []string{slug, "--skill", "example-skill"}
	listed := invoke(0, "https://github.com/acme/skills.git", "--list")
	if len(listed.Skills) != 2 || listed.Skills[0].Description == "" {
		t.Fatal(listed)
	}
	if auto := invoke(0, slug); len(auto.Library) != 0 || len(auto.Skills) != 2 {
		t.Fatal(auto)
	}
	if bad := invoke(1, selected...); !strings.Contains(bad.Library[0].Detail, "license") {
		t.Fatal(bad)
	}
	write(filepath.Join(repo, "LICENSE"), "MIT\n")
	git("add", ".")
	git("commit", "-qm", "license")
	sha := git("rev-parse", "HEAD")
	git("branch", "release/v1")
	tree := "https://github.com/acme/skills/tree/release/v1/skills/example-skill"
	preview := invoke(0, tree, "-C", p, "--dry-run")
	if preview.Commit != sha || preview.Project != p || preview.Library[0].Action != "would_import" || len(preview.Install) != 1 || !slices.Contains(preview.ApplyArgs, sha) {
		t.Fatal(preview)
	}
	target := filepath.Join(lib, "skills", "example-skill")
	if core.Lexists(target) || core.Lexists(core.Dest(p, "example-skill")) {
		t.Fatal("dry run changed destination")
	}
	t.Run("project preview refuses unreadable manifest and occupied parent", func(t *testing.T) {
		for _, obstacle := range []string{filepath.Join(".agents", core.Manifest), filepath.Join(".agents", "skills"), ".claude"} {
			proj := t.TempDir()
			path := filepath.Join(proj, obstacle)
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			write(path, "occupied")
			var report importReport
			rc := runJSON(t, &report, "import", tree, "-C", proj, "--dry-run")
			if rc != 1 || len(report.ApplyArgs) != 0 || core.Lexists(target) || core.Lexists(core.Dest(proj, "example-skill")) {
				t.Errorf("%s: rc=%d report=%+v", obstacle, rc, report)
			}
		}
	})
	multi := invoke(0, slug, "--skill", "example-skill", "--skill", "second-skill", "--dry-run")
	if len(multi.Library) != 2 || len(multi.Install) != 0 {
		t.Fatal(multi)
	}
	mkskill(t, repo, "example-skill", "Changed in next upstream version")
	git("add", ".")
	git("commit", "-qm", "next")
	// Fail the project phase after the pinned library import succeeds.
	obstacle := core.LinkP(p, "example-skill")
	if err := os.MkdirAll(obstacle, 0755); err != nil {
		t.Fatal(err)
	}
	failed := invoke(1, preview.ApplyArgs[1:]...)
	if failed.Library[0].Action != "imported" || failed.Install[0].OK || !slices.Equal(failed.RetryArgs, []string{"add", "-C", p, "example-skill"}) {
		t.Fatal(failed)
	}
	if data := core.LoadMap(filepath.Join(target, "SOURCE.json")); data["commit"] != sha || data["subpath"] != "skills/example-skill" {
		t.Fatal(data)
	}
	if err := os.Remove(obstacle); err != nil {
		t.Fatal(err)
	}
	if rc, _, e := run(t, failed.RetryArgs...); rc != 0 {
		t.Fatal(e)
	}
	again := invoke(0, preview.ApplyArgs[1:]...)
	if again.Library[0].Action != "reused" || !again.Install[0].OK {
		t.Fatal(again)
	}
	if rc, _, e := run(t, "verify", "-C", p); rc != 0 {
		t.Fatal(e)
	}
	if conflict := invoke(1, selected...); !strings.Contains(conflict.Library[0].Detail, "version conflict") {
		t.Fatal(conflict)
	}
	write(filepath.Join(target, "SKILL.md"), "edited")
	if conflict := invoke(1, preview.ApplyArgs[1:]...); !strings.Contains(conflict.Library[0].Detail, "local modifications") {
		t.Fatal(conflict)
	}
	if body, _ := os.ReadFile(filepath.Join(target, "SKILL.md")); string(body) != "edited" {
		t.Fatal("overwrote edit")
	}
	mkskill(t, lib, "second-skill", "Local namesake")
	if conflict := invoke(1, slug, "--skill", "second-skill"); !strings.Contains(conflict.Library[0].Detail, "different source") {
		t.Fatal(conflict)
	}
	mkskill(t, filepath.Join(repo, "other"), "second-skill", "Duplicate upstream name")
	git("add", ".")
	git("commit", "-qm", "duplicate")
	if ambiguous := invoke(1, slug, "--skill", "second-skill"); !strings.Contains(ambiguous.Error, "ambiguous") {
		t.Fatal(ambiguous)
	}
	write(filepath.Join(repo, "SKILL.md"), "---\nname: root-example\ndescription: Root skill\nhooks:\n  Stop: []\n---\n")
	git("add", ".")
	git("commit", "-qm", "root skill with review finding")
	audit := invoke(1, slug, "--skill", "root-example", "--dry-run")
	if !strings.Contains(audit.Library[0].Detail, "audit review") || len(audit.ApplyArgs) != 0 {
		t.Fatal(audit)
	}
	allowed := invoke(0, slug, "--skill", "root-example", "--allow-risky", "--dry-run")
	if !slices.Contains(allowed.ApplyArgs, "--allow-risky") {
		t.Fatal(allowed)
	}
	if unreachable := invoke(1, "missing/repo"); unreachable.Error == "" {
		t.Fatal(unreachable)
	}
	if _, err := os.Stat(filepath.Join(lib, "IMPORT_LOG.md")); !os.IsNotExist(err) {
		t.Fatal("touched queue import log")
	}
}
