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
