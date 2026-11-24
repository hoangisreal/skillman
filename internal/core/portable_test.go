package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPortableGitLinkMigration(t *testing.T) {
	p := t.TempDir()
	sh(t, p, "git", "init", "-q")
	name := "example"
	source := mkskill(t, filepath.Dir(Dest(p, name)), name, "", "", nil)
	m := ReadManifest(p)
	m.top.Set("claude_mode", "symlink")
	m.Skills[name] = &Entry{TreeHash: TreeHash(source)}
	if err := WriteManifest(p, m); err != nil {
		t.Fatal(err)
	}
	lp := LinkP(p, name)
	if err := os.MkdirAll(filepath.Dir(lp), 0755); err != nil {
		t.Fatal(err)
	}
	target := "../../.agents/skills/" + name
	if err := os.WriteFile(lp, []byte(target), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertCopies(p, false); err == nil {
		t.Fatal("accepted an untracked text file")
	}
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = p
	cmd.Stdin = strings.NewReader(target)
	hash, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	sh(t, p, "git", "update-index", "--add", "--cacheinfo", "120000,"+strings.TrimSpace(string(hash))+",.claude/skills/"+name)
	if _, err := ConvertCopies(p, true); err != nil || IsDir(lp) {
		t.Fatal("preview", err)
	}
	if _, err := ConvertCopies(p, false); err != nil || !LinkOK(p, name) {
		t.Fatal("conversion", err)
	}
}

func TestPortableLifecycle(t *testing.T) {
	if p := os.Getenv("SKILLMAN_TEST_CHILD_PROJECT"); p != "" {
		name := os.Getenv("SKILLMAN_TEST_CHILD_NAME")
		rs, err := Install(p, []Item{{Name: name}}, false)
		if err != nil || len(rs) != 1 || !rs[0].OK {
			t.Fatal(rs, err)
		}
		return
	}
	root := t.TempDir()
	p := filepath.Join(root, "Dự án có khoảng trắng")
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := ConvertCopies(p, false); err != nil {
		t.Fatal(err)
	}
	var cmds []*exec.Cmd
	for _, name := range []string{"alpha", "beta"} {
		mkskill(t, SKILLS, name, "", "", nil)
		cmd := exec.Command(os.Args[0], "-test.run=^TestPortableLifecycle$")
		cmd.Env = append(os.Environ(), "SKILLMAN_TEST_CHILD_PROJECT="+p, "SKILLMAN_TEST_CHILD_NAME="+name)
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stdout
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if len(ReadManifest(p).Skills) != 2 || !LinkOK(p, "alpha") || IsSymlink(LinkP(p, "alpha")) {
		t.Fatal("missing copy or manifest entry")
	}
	mkskill(t, SKILLS, "alpha", "changed", "", nil)
	rs, err := UpdateProject(p, nil, false)
	if err != nil || len(rs) != 1 || !rs[0].OK || !LinkOK(p, "alpha") {
		t.Fatal(rs, err)
	}
	file := filepath.Join(LinkP(p, "alpha"), "SKILL.md")
	if err := os.WriteFile(file, []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	rs, err = Uninstall(p, []string{"alpha"})
	if err != nil || rs[0].OK || !Exists(Dest(p, "alpha")) {
		t.Fatal(rs, err)
	}
	rs, err = Uninstall(p, []string{"beta"})
	if err != nil || !rs[0].OK {
		t.Fatal(rs, err)
	}
	for _, e := range ListTrash("") {
		if e.S("name") == "beta" {
			if _, err := RestoreTrash(e.S("slot")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !LinkOK(p, "beta") {
		t.Fatal("restore did not recreate copy")
	}
}

func TestPortableClaudeModeRoundTrip(t *testing.T) {
	project := t.TempDir()
	t.Setenv("SKILLMAN_STATE", t.TempDir())
	t.Setenv("SKILL_LIBRARY", t.TempDir())
	Configure()
	mkskill(t, SKILLS, "example", "A test skill", "", nil)
	if _, err := ConvertClaudeMode(project, "copy", false); err != nil {
		t.Fatal(err)
	}
	if results, err := Install(project, []Item{{Name: "example"}}, false); err != nil || !results[0].OK {
		t.Fatal(results, err)
	}
	path := LinkP(project, "example")
	before := TreeHash(project)
	if paths, err := ConvertClaudeMode(project, "symlink", true); err != nil || len(paths) != 1 || paths[0] != path || TreeHash(project) != before {
		t.Fatal(paths, err)
	}
	probe := filepath.Join(t.TempDir(), "probe")
	probeErr := os.Symlink(Dest(project, "example"), probe)
	_, err := ConvertClaudeMode(project, "symlink", false)
	if probeErr != nil {
		if err == nil || IsSymlink(path) || !LinkOK(project, "example") || TreeHash(project) != before {
			t.Fatalf("failed symlink creation should roll back: %v", err)
		}
		return
	}
	if err != nil || !IsSymlink(path) || !LinkOK(project, "example") {
		t.Fatalf("copy to symlink: %v", err)
	}
	if _, err := ConvertClaudeMode(project, "copy", false); err != nil || IsSymlink(path) || !LinkOK(project, "example") {
		t.Fatalf("symlink to copy: %v", err)
	}
}

func TestPortableSymlinkModeCheckout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	mkskill(t, SKILLS, "example", "A test skill", "line one\nline two\n", nil)
	p := filepath.Join(root, "app")
	must(t, os.MkdirAll(p, 0o755))
	sh(t, p, "git", "init", "-q")
	if _, err := ConvertClaudeMode(p, "symlink", false); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if rs, err := Install(p, []Item{{Name: "example"}}, false); err != nil || !allOK(rs) {
		t.Fatal(rs, err)
	}
	sh(t, p, "git", "add", "-A")
	sh(t, p, "git", "commit", "-qm", "skills")
	// A teammate whose Git converts line endings on checkout must still verify.
	clone := filepath.Join(root, "clone")
	sh(t, root, "git", "-c", "core.autocrlf=true", "-c", "core.symlinks=true", "clone", "-q", p, clone)
	if rs := Verify(clone); !allOK(rs) {
		t.Fatalf("verify after autocrlf checkout: %+v", rs)
	}
}

func TestPortableMaterializeKeepsGitBytes(t *testing.T) {
	f, _ := newFixture(t, []string{"alpha"}, nil)
	must(t, os.WriteFile(filepath.Join(f.up, "skills", "alpha", ".gitattributes"), []byte("* text=auto\n"), 0o644))
	c := f.push("normalize text")
	// Git for Windows defaults to CRLF for text=auto files.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.eol")
	t.Setenv("GIT_CONFIG_VALUE_0", "crlf")
	staged, _, err := Materialize("acme/skills", c, "skills/alpha", "alpha", "", nil)
	must(t, err)
	b, err := os.ReadFile(filepath.Join(staged, "SKILL.md"))
	must(t, err)
	if strings.Contains(string(b), "\r\n") {
		t.Fatalf("checkout converted line endings: %q", b)
	}
}

func TestPortableRelinkKeepsDirectoryLink(t *testing.T) {
	p := t.TempDir()
	mkskill(t, filepath.Join(p, ".agents", "skills"), "example", "", "", nil)
	lp := LinkP(p, "example")
	must(t, os.MkdirAll(filepath.Dir(lp), 0o755))
	if err := os.Symlink("../../.agents/skills/elsewhere", lp); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	// Replacing a link must still give clients a folder they can read.
	must(t, setSkillLink(lp, "../../.agents/skills/example"))
	if !IsFile(filepath.Join(lp, "SKILL.md")) || !isDirLink(lp) {
		t.Fatal("replaced link is not a folder link")
	}
	for _, e := range SortedDir(filepath.Dir(lp)) {
		if e.Name() != "example" {
			t.Fatalf("left behind %s", e.Name())
		}
	}
}

func TestPortableRefusesSkillSymlinks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	src := mkskill(t, SKILLS, "linked", "", "", map[string]string{"refs/a.md": "a\n"})
	if err := os.Symlink("refs/a.md", filepath.Join(src, "alias.md")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	p := filepath.Join(root, "app")
	must(t, os.MkdirAll(p, 0o755))
	// Git for Windows checks links out as plain files, so the copy would not verify there.
	preview, err := PreviewInstall(p, []Item{{Name: "linked"}}, false)
	must(t, err)
	if preview.OK || preview.Skills[0].Action != "blocked" || !strings.Contains(preview.Skills[0].Reason, "alias.md") {
		t.Fatalf("preview: %+v", preview.Skills)
	}
	if rs, err := Install(p, []Item{{Name: "linked"}}, false); err != nil || rs[0].OK || Lexists(Dest(p, "linked")) {
		t.Fatalf("install: %v %+v", err, rs)
	}
	must(t, CopyTree(src, Dest(p, "linked"), nil))
	if rs, err := Adopt(p, []string{"linked"}); err != nil || rs[0].OK || ReadManifest(p).Skills["linked"] != nil {
		t.Fatalf("adopt: %v %+v", err, rs)
	}
	must(t, os.RemoveAll(Dest(p, "linked")))
	plain := mkskill(t, SKILLS, "plain", "", "", nil)
	if rs, err := Install(p, []Item{{Name: "plain"}}, false); err != nil || !rs[0].OK {
		t.Fatalf("install plain: %v %+v", err, rs)
	}
	must(t, os.Symlink("SKILL.md", filepath.Join(plain, "alias.md")))
	if rs, err := PreviewProjectUpdate(p, []string{"plain"}, false); err != nil || rs[0].OK {
		t.Fatalf("update preview: %v %+v", err, rs)
	}
	if rs, err := UpdateProject(p, []string{"plain"}, false); err != nil || rs[0].OK || Lexists(filepath.Join(Dest(p, "plain"), "alias.md")) {
		t.Fatalf("update: %v %+v", err, rs)
	}
}

// An older Linux project (symlink mode, no .gitattributes) checked out by Git
// for Windows defaults: CRLF text and plain-file link placeholders.
func windowsCheckoutOfOldLinuxProject(t *testing.T) (root, clone string) {
	root = t.TempDir()
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	mkskill(t, SKILLS, "example", "A test skill", "line one\nline two\n", map[string]string{"refs/a.md": "a\nb\n"})
	p := filepath.Join(root, "app")
	must(t, os.MkdirAll(p, 0o755))
	sh(t, p, "git", "init", "-q")
	if _, err := ConvertClaudeMode(p, "symlink", false); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if rs, err := Install(p, []Item{{Name: "example"}}, false); err != nil || !allOK(rs) {
		t.Fatal(rs, err)
	}
	for _, dir := range []string{".agents/skills", ".claude/skills"} {
		must(t, os.Remove(filepath.Join(p, dir, ".gitattributes")))
	}
	sh(t, p, "git", "add", "-A")
	sh(t, p, "git", "commit", "-qm", "skills")
	clone = filepath.Join(root, "clone")
	sh(t, root, "git", "-c", "core.autocrlf=true", "-c", "core.symlinks=false", "clone", "-q", p, clone)
	sh(t, clone, "git", "config", "core.autocrlf", "true")
	sh(t, clone, "git", "config", "core.symlinks", "false")
	return root, clone
}

func TestPortableConvertsWindowsCheckoutOfLinuxProject(t *testing.T) {
	_, clone := windowsCheckoutOfOldLinuxProject(t)
	b, _ := os.ReadFile(filepath.Join(Dest(clone, "example"), "SKILL.md"))
	if !strings.Contains(string(b), "\r\n") {
		t.Skip("this Git did not convert line endings")
	}
	if rs := Verify(clone); allOK(rs) || !strings.Contains(rs[0].Message, "--mode copy") {
		t.Fatalf("verify should explain the checkout conversion: %+v", rs)
	}
	if _, err := ConvertClaudeMode(clone, "copy", false); err != nil {
		t.Fatal("conversion of a clean checkout:", err)
	}
	if rs := Verify(clone); !allOK(rs) {
		t.Fatalf("verify after conversion: %+v", rs)
	}
	if !Exists(filepath.Join(clone, ".agents/skills/.gitattributes")) {
		t.Fatal("conversion did not add .gitattributes")
	}
}

func TestPortableKeepsRealEditsInWindowsCheckout(t *testing.T) {
	_, clone := windowsCheckoutOfOldLinuxProject(t)
	edited := filepath.Join(Dest(clone, "example"), "refs", "a.md")
	must(t, os.WriteFile(edited, []byte("local change\r\n"), 0o644))
	if _, err := ConvertClaudeMode(clone, "copy", false); err == nil {
		t.Fatal("converted over a real edit")
	}
	if b, _ := os.ReadFile(edited); string(b) != "local change\r\n" {
		t.Fatalf("edit lost: %q", b)
	}
}

func TestPortableRefusesSkillTextAttributes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	mkskill(t, SKILLS, "converts", "", "", map[string]string{".gitattributes": "* text=auto\n"})
	mkskill(t, SKILLS, "binary-only", "", "", map[string]string{"assets/.gitattributes": "*.png binary\n* -text\n"})
	p := filepath.Join(root, "app")
	must(t, os.MkdirAll(p, 0o755))
	rs, err := Install(p, []Item{{Name: "converts"}, {Name: "binary-only"}}, false)
	must(t, err)
	if rs[0].OK || !strings.Contains(rs[0].Message, ".gitattributes") || Lexists(Dest(p, "converts")) {
		t.Fatalf("text=auto skill: %+v", rs[0])
	}
	if !rs[1].OK {
		t.Fatalf("-text and binary rules are safe: %+v", rs[1])
	}
}

func TestPortableKeepsHiddenEditsInWindowsCheckout(t *testing.T) {
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		_, clone := windowsCheckoutOfOldLinuxProject(t)
		edited := filepath.Join(Dest(clone, "example"), "refs", "a.md")
		sh(t, clone, "git", "update-index", flag, ".agents/skills/example/refs/a.md")
		must(t, os.WriteFile(edited, []byte("local change\r\n"), 0o644))
		if _, err := ConvertClaudeMode(clone, "copy", false); err == nil {
			t.Fatalf("%s: converted over a hidden edit", flag)
		}
		if b, _ := os.ReadFile(edited); string(b) != "local change\r\n" {
			t.Fatalf("%s: edit lost: %q", flag, b)
		}
	}
}

func TestPortableIgnoredFileIsNotACheckoutConversion(t *testing.T) {
	_, clone := windowsCheckoutOfOldLinuxProject(t)
	must(t, os.WriteFile(filepath.Join(clone, ".gitignore"), []byte("*.log\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(Dest(clone, "example"), "run.log"), []byte("x"), 0o644))
	if _, ok := checkoutConverted(clone, Dest(clone, "example"), ReadManifest(clone).Skills["example"].TreeHash); ok {
		t.Fatal("an ignored extra file was reported as a checkout conversion")
	}
}

func TestPortableTextAttributeRules(t *testing.T) {
	for attrs, unsafe := range map[string]bool{
		"* text=auto\n":   true,
		"* !text\n":       true,
		"*.md eol=crlf\n": true,
		"* -text\n":       false,
		"*.png binary\n":  false,
		"# keep text files byte-exact\n* -text\n": false,
	} {
		if got := unsafeTextAttributes([]byte(attrs)); got != unsafe {
			t.Errorf("%q: unsafe=%v, want %v", attrs, got, unsafe)
		}
	}
}
