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
