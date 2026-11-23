package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

func runOK(t *testing.T, args ...string) {
	t.Helper()
	if rc, out, errOut := run(t, args...); rc != 0 {
		t.Fatalf("%v exited %d: %s%s", args, rc, out, errOut)
	}
}

func TestEmptyProjectStackWorkflow(t *testing.T) {
	lib, _ := setup(t)
	project := t.TempDir()
	mkskill(t, lib, "golang-testing", "Testing Go modules")
	runOK(t, "library", "rebuild-index")

	var rec search.Recommendation
	if rc := runJSON(t, &rec, "recommend", "-C", project, "--brief"); rc != 0 || !strings.Contains(rec.Message, "No project stack detected") || len(rec.Shortlist) == 0 || rec.Shortlist[0].Evidence != "baseline" {
		t.Fatalf("baseline: %d %+v", rc, rec)
	}
	rec = search.Recommendation{}
	if rc := runJSON(t, &rec, "recommend", "-C", project, "--stack", "vue,postgres", "--stack", "vue", "--brief"); rc != 0 {
		t.Fatalf("declared stacks: %d %+v", rc, rec)
	}
	evidence := map[string]bool{}
	for _, choice := range rec.Shortlist {
		evidence[choice.Evidence] = true
	}
	if len(evidence) != 2 || !evidence["declared:vue"] || !evidence["declared:postgres"] {
		t.Fatalf("declared evidence: %v", evidence)
	}
	rec = search.Recommendation{}
	if rc := runJSON(t, &rec, "recommend", "-C", project, "--stack", "go", "--task", "testing", "--brief"); rc != 0 || len(rec.Shortlist) != 1 || rec.Shortlist[0].Name != "golang-testing" || rec.Shortlist[0].Evidence != "declared:go" {
		t.Fatalf("packless Go: %d %+v", rc, rec)
	}
	args := slices.Clone(rec.InstallArgs)
	rec = search.Recommendation{}
	if rc := runJSON(t, &rec, "recommend", "-C", project, "--stack", "php", "--brief"); rc != 0 || len(rec.InstallArgs) != 0 || rec.Message == "" {
		t.Fatalf("missing local skills: %d %+v", rc, rec)
	}
	if rc, _, _ := run(t, "recommend", "-C", project, "--stack", "unknown-stack"); rc != 2 {
		t.Fatalf("unknown stack exited %d", rc)
	}
	runOK(t, append(slices.Clone(args), "--dry-run")...)
	entries, err := os.ReadDir(project)
	if err != nil || len(entries) != 0 {
		t.Fatalf("preview changed empty project: %v %v", entries, err)
	}
	runOK(t, args...)
	runOK(t, "verify", "-C", project)
}

func TestProjectInstallPreviewAndManifest(t *testing.T) {
	lib, project := setup(t)
	runOK(t, "repair-links", "-C", project, "--mode", "copy")
	mirror := core.LinkP(project, "tdd")
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		t.Fatal(err)
	}
	before := core.TreeHash(project)
	var preview core.InstallPreview
	if rc := runJSON(t, &preview, "add", "-C", project, "tdd", "--force", "--dry-run"); rc != 0 || preview.OK || len(preview.Skills) != 1 || preview.Skills[0].Action != "blocked" || preview.AddedTokens != 0 {
		t.Fatalf("blocked preview: %d %+v", rc, preview)
	}
	if rc, _, _ := run(t, "add", "-C", project, "tdd", "--force"); rc != 1 {
		t.Fatalf("blocked install exited %d", rc)
	}
	if got := core.TreeHash(project); got != before || len(core.ListTrash("")) != 0 {
		t.Fatal("blocked operation changed the project or trash")
	}

	runOK(t, "add", "-C", project, "vitest")
	oldBudget := core.TokenBudgetOf(project)
	oldTokens := core.SkillTokens(core.Dest(project, "vitest"))
	mkskill(t, lib, "vitest", strings.Repeat("new details ", 10)+"for Vitest")
	wantBudget := oldBudget.Total - oldTokens + core.SkillTokens(filepath.Join(lib, "skills", "vitest"))
	before = core.TreeHash(project)
	if rc := runJSON(t, &preview, "add", "-C", project, "vitest", "--force", "--dry-run"); rc != 0 || !preview.OK || preview.Skills[0].Action != "replace" || preview.AddedTokens != 0 || preview.BudgetAfter != wantBudget {
		t.Fatalf("replace preview: %d %+v, budget want %d", rc, preview, wantBudget)
	}
	if core.TreeHash(project) != before || len(core.ListTrash("")) != 0 {
		t.Fatal("replace preview changed the project or trash")
	}
	targets := preview.Skills[0].Targets
	if len(targets) != 2 || targets[0].Path != ".agents/skills/vitest" || targets[1].Path != ".claude/skills/vitest" || targets[1].Mode != "copy" {
		t.Fatalf("unexpected preview targets: %+v", targets)
	}
	runOK(t, "add", "-C", project, "vitest", "--force")
	runOK(t, "verify", "-C", project)

	empty := t.TempDir()
	dir := filepath.Join(empty, ".agents")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, core.Manifest)
	for _, bad := range []string{"{broken", `{"skills":[]}`, `{"skills":{"../example":null}}`} {
		if err := os.WriteFile(manifest, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"verify"}, {"doctor"}, {"add", "vitest", "--dry-run"}, {"add", "vitest"}, {"update"}} {
			if rc, _, _ := run(t, append(slices.Clone(args), "-C", empty)...); rc != 1 {
				t.Fatalf("%v with %q exited %d", args, bad, rc)
			}
		}
		if b, err := os.ReadFile(manifest); err != nil || string(b) != bad || core.Exists(core.Dest(empty, "vitest")) {
			t.Fatal("corrupt manifest or skill files were changed")
		}
	}
	legacy := `{"skills":{},"custom":{"keep":true}}`
	if err := os.WriteFile(manifest, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	runOK(t, "verify", "-C", empty)
	runOK(t, "repair-links", "-C", empty, "--mode", "copy")
	runOK(t, "add", "-C", empty, "vitest")
	if b, err := os.ReadFile(manifest); err != nil || !strings.Contains(string(b), `"custom":`) {
		t.Fatal("legacy extension was lost")
	}
}

func TestProjectAgentStatus(t *testing.T) {
	_, project := setup(t)
	home := filepath.Join(filepath.Dir(project), "home")
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	runOK(t, "repair-links", "-C", project, "--mode", "copy")
	runOK(t, "add", "-C", project, "vitest", "tdd")
	mkskill(t, project, "unrelated", "Outside the recognized paths")
	for _, dir := range []string{filepath.Join(project, ".cursor"), filepath.Join(home, ".agents")} {
		mkskill(t, dir, "vitest", "Alternate Vitest guide")
	}
	if err := os.WriteFile(filepath.Join(core.LinkP(project, "tdd"), "SKILL.md"), []byte("---\nname: tdd\ndescription: locally edited\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(core.Dest(project, "tdd"), filepath.Join(project, "tdd-saved")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SKILL_LIBRARY", filepath.Join(t.TempDir(), "absent-library"))
	core.Configure()
	read := func(extra ...string) map[string]core.AgentStatus {
		t.Helper()
		var report core.AgentStatusReport
		if rc := runJSON(t, &report, append([]string{"status", "-C", project, "--agents"}, extra...)...); rc != 0 || len(report.Agents) != 5 {
			t.Fatalf("agent status: %d %+v", rc, report)
		}
		result := map[string]core.AgentStatus{}
		for _, row := range report.Agents {
			result[row.ID] = row
		}
		return result
	}
	local := read()
	codex := local["codex"]
	if len(codex.Duplicates) != 0 || len(codex.Skills) != 2 || codex.Skills[0].State != "missing" {
		t.Fatalf("project-only Codex: %+v", codex)
	}
	claude := local["claude-code"]
	if len(claude.Skills) != 2 || claude.Skills[0].State != "drifted" || claude.Skills[1].State != "available" {
		t.Fatalf("Claude mirrors: %+v", claude)
	}
	cursor := local["cursor"]
	if len(cursor.Duplicates) != 1 || len(cursor.Duplicates[0].Paths) != 2 {
		t.Fatalf("Cursor duplicate or mirror alias: %+v", cursor)
	}
	opencode := local["opencode"]
	if len(opencode.Duplicates) != 0 || !slices.ContainsFunc(opencode.Skills, func(s core.AgentSkill) bool {
		return s.Name == "vitest" && slices.Contains(s.Aliases, ".claude/skills/vitest")
	}) {
		t.Fatalf("OpenCode shared skill: %+v", opencode)
	}
	global := read("--include-global")
	if dups := global["codex"].Duplicates; len(dups) != 1 || dups[0].Name != "vitest" {
		t.Fatalf("global duplicates: %+v", global["codex"])
	}
	var old []core.SkillStatus
	if rc := runJSON(t, &old, "status", "-C", project); rc != 0 || len(old) != 2 {
		t.Fatalf("legacy status: %d %+v", rc, old)
	}
}

func TestProjectVerifyClaudeLinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires project symlinks")
	}
	lib, project := setup(t)
	runOK(t, "add", "-C", project, "vitest")
	runOK(t, "verify", "-C", project)
	link := core.LinkP(project, "vitest")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(lib, "skills", "vitest"), link); err != nil {
		t.Fatal(err)
	}
	if rc, _, _ := run(t, "verify", "-C", project); rc != 1 {
		t.Fatalf("misdirected symlink passed verification: %d", rc)
	}
	var report core.AgentStatusReport
	if rc := runJSON(t, &report, "status", "-C", project, "--agents"); rc != 0 || len(report.Agents[0].Skills) != 1 || report.Agents[0].Skills[0].State != "external_link" {
		t.Fatalf("external link: %d %+v", rc, report)
	}
	runOK(t, "repair-links", "-C", project)
	runOK(t, "verify", "-C", project)
}

func TestProjectRecovery(t *testing.T) {
	lib, project := setup(t)
	runOK(t, "repair-links", "-C", project, "--mode", "copy")
	runOK(t, "add", "-C", project, "vitest")
	canon := filepath.Join(core.Dest(project, "vitest"), "SKILL.md")
	mirror := filepath.Join(core.LinkP(project, "vitest"), "SKILL.md")
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	write := func(path, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	projectEdit := read(canon) + "\nEdited in the project."
	write(canon, projectEdit)
	runOK(t, "repair-links", "-C", project)
	if read(mirror) != projectEdit {
		t.Fatal("repair did not refresh the unchanged Claude copy")
	}
	runOK(t, "pin", "-C", project, "vitest")
	runOK(t, "verify", "-C", project)

	mirrorEdit := read(mirror) + "\nIndependent Claude edit."
	write(mirror, mirrorEdit)
	if rc, _, err := run(t, "verify", "-C", project); rc != 1 || !strings.Contains(err, "Claude copy has local changes") {
		t.Fatalf("verify: rc=%d %s", rc, err)
	}
	var blocked struct{ Error string }
	if rc := runJSON(t, &blocked, "repair-links", "-C", project); rc != 1 || !strings.Contains(blocked.Error, "Claude copy has local changes") {
		t.Fatalf("repair: rc=%d %+v", rc, blocked)
	}
	if read(mirror) != mirrorEdit || read(canon) != projectEdit {
		t.Fatal("repair changed user edits")
	}
	write(mirror, projectEdit)
	runOK(t, "unpin", "-C", project, "vitest")
	runOK(t, "add", "-C", project, "vitest", "--force")
	runOK(t, "verify", "-C", project)

	runOK(t, "add", "-C", project, "tdd")
	runOK(t, "pin", "-C", project, "tdd")
	libraryTDD := filepath.Join(lib, "skills", "tdd", "SKILL.md")
	original := read(libraryTDD)
	write(libraryTDD, original+"\nLibrary revision.")
	var affected struct{ Projects []core.Affected }
	if rc := runJSON(t, &affected, "update-projects", "--dry-run"); rc != 0 || affected.Projects == nil || len(affected.Projects) != 0 {
		t.Fatalf("pinned skill should not be offered for a project update: rc=%d %+v", rc, affected)
	}
	runOK(t, "update-projects")
	write(libraryTDD, original)
	runOK(t, "unpin", "-C", project, "tdd")
	if err := os.RemoveAll(core.Dest(project, "tdd")); err != nil {
		t.Fatal(err)
	}
	if rc, _, err := run(t, "update", "-C", project, "tdd", "--dry-run"); rc != 1 || !strings.Contains(err, "skillman add") {
		t.Fatalf("missing copy guidance: rc=%d %s", rc, err)
	}
	runOK(t, "update", "-C", project, "tdd", "--force", "--dry-run")
	if core.Lexists(core.Dest(project, "tdd")) {
		t.Fatal("preview restored a copy")
	}
	runOK(t, "update", "-C", project, "tdd", "--force")
	runOK(t, "verify", "-C", project)

	invalid := core.Dest(project, "review-pr")
	if err := os.Mkdir(invalid, 0755); err != nil {
		t.Fatal(err)
	}
	if rc, _, msg := run(t, "adopt", "-C", project, "review-pr"); rc != 1 || !strings.Contains(msg, "regular SKILL.md") {
		t.Fatalf("adopt invalid directory: rc=%d %s", rc, msg)
	}
	if core.Lexists(core.LinkP(project, "review-pr")) || core.ReadManifest(project).Skills["review-pr"] != nil {
		t.Fatal("adopt recorded an invalid directory")
	}
	if err := os.Remove(invalid); err != nil {
		t.Fatal(err)
	}

	stateFile := filepath.Join(t.TempDir(), "unusable-state")
	write(stateFile, "Force the in-project fallback")
	t.Setenv("SKILLMAN_STATE", stateFile)
	core.Configure()
	runOK(t, "remove", "-C", project, "tdd")
	var trash struct{ Entries []core.TrashEntry }
	if rc := runJSON(t, &trash, "trash", "list", "-C", project); rc != 0 || len(trash.Entries) != 1 {
		t.Fatalf("fallback trash: rc=%d %+v", rc, trash)
	}
	id := trash.Entries[0].S("id")
	var restored struct{ ID string }
	if rc := runJSON(t, &restored, "trash", "restore", id, "-C", project); rc != 0 || restored.ID != id {
		t.Fatalf("restore fallback trash: rc=%d %+v", rc, restored)
	}
	runOK(t, "verify", "-C", project)
	runOK(t, "remove", "-C", project, "tdd")
	var empty struct{ Deleted int }
	if rc := runJSON(t, &empty, "trash", "empty", "--yes", "-C", project); rc != 0 || empty.Deleted != 1 {
		t.Fatalf("empty fallback trash: rc=%d %+v", rc, empty)
	}
	if len(core.ListTrash(project)) != 0 {
		t.Fatal("trash empty left fallback entries")
	}
}

func TestTrashReportsFailedDeletion(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX file permissions as a non-root user")
	}
	setup(t)
	runOK(t, "pack", "create", "qa-trash", "vitest")
	runOK(t, "pack", "delete", "qa-trash")
	entries := core.ListTrash("")
	if len(entries) != 1 {
		t.Fatalf("expected one trash entry, got %+v", entries)
	}
	slot := entries[0].S("slot")
	if err := os.Chmod(slot, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(slot, 0755)
	var report struct {
		Deleted int
		Error   string
	}
	id := entries[0].S("id")
	if rc := runJSON(t, &report, "trash", "empty", "--yes", id); rc != 1 || report.Deleted != 0 || report.Error == "" {
		t.Fatalf("read-only trash deletion: rc=%d %+v", rc, report)
	}
	if !core.IsFile(filepath.Join(slot, "qa-trash.yaml")) {
		t.Fatal("failed deletion did not preserve the trashed pack")
	}
	if err := os.Chmod(slot, 0755); err != nil {
		t.Fatal(err)
	}
	runOK(t, "trash", "restore", id)
	runOK(t, "pack", "show", "qa-trash")
}
