package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "skills" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

func TestOccupiedReasonNamesSandboxMasks(t *testing.T) {
	// Sandboxes bind /dev/null over protected paths such as .claude/skills.
	got := occupiedReason(".claude/skills", fakeInfo{fs.ModeDevice | fs.ModeCharDevice})
	if !strings.Contains(got, "masked by a device file") || !strings.Contains(got, "excludedCommands") {
		t.Fatal(got)
	}
	if got := occupiedReason(".claude/skills", fakeInfo{0}); !strings.Contains(got, "is a file") {
		t.Fatal(got)
	}
}

func TestDryRunReportsReadOnlyTargets(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions as a regular user")
	}
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	mkskill(t, SKILLS, "example", "", "", nil)
	p := filepath.Join(root, "app")
	must(t, os.MkdirAll(filepath.Join(p, ".agents"), 0o755))
	must(t, os.MkdirAll(STATE, 0o755))
	must(t, os.Chmod(filepath.Join(p, ".agents"), 0o555))
	must(t, os.Chmod(STATE, 0o555))
	defer os.Chmod(filepath.Join(p, ".agents"), 0o755)
	defer os.Chmod(STATE, 0o755)
	preview, err := PreviewInstall(p, []Item{{Name: "example"}}, false)
	must(t, err)
	if preview.OK || preview.Skills[0].Action != "blocked" || !strings.Contains(preview.Skills[0].Reason, "cannot write") {
		t.Fatalf("read-only project: %+v", preview.Skills)
	}
	if len(preview.Warnings) != 1 || !strings.Contains(preview.Warnings[0], "registry") {
		t.Fatalf("read-only state: %v", preview.Warnings)
	}
}

func TestAgentStatusExplainsInvalidSkills(t *testing.T) {
	// Resolve like ResolveProject: macOS and Windows temp dirs go through links.
	root, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "lib"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	Configure()
	p := filepath.Join(root, "app")
	skills := filepath.Join(p, ".agents", "skills")
	mkskill(t, skills, "renamed", "", "", nil)
	must(t, os.Rename(filepath.Join(skills, "renamed"), filepath.Join(skills, "other-name")))
	must(t, os.MkdirAll(filepath.Join(skills, "empty"), 0o755))
	report, err := InspectAgents(p, false)
	must(t, err)
	reasons := map[string]string{}
	for _, a := range report.Agents {
		if a.ID != "codex" {
			continue
		}
		for _, s := range a.Skills {
			reasons[s.Name] = s.State + ": " + s.Reason
		}
	}
	if !strings.Contains(reasons["other-name"], `name is "renamed"`) || !strings.Contains(reasons["empty"], "no regular SKILL.md") {
		t.Fatalf("reasons: %v", reasons)
	}
}
