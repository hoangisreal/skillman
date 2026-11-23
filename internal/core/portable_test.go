package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
