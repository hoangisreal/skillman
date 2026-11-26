package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	skillman "github.com/hoangisreal/skillman"
	"github.com/hoangisreal/skillman/internal/core"
)

// SelfCheck is one finding of doctor --self.
type SelfCheck struct {
	Level   string `json:"level"` // ok | info | warn | error
	Check   string `json:"check"`
	Message string `json:"message"`
	Fix     string `json:"fix,omitempty"`
}

// libraryReadme is written by init into a library that has no README or the Python-era one.
var libraryReadme = strings.ReplaceAll(`# Skill library

This folder is a Skillman library: skills that no agent scans until you install them into a project.
It is its own Git repository, so every library change can be reviewed and reverted.

| Task | Command |
| --- | --- |
| Check this install, PATH links and the library | §skillman doctor --self§ |
| Find skills | §skillman search <words>§ · §skillman info <name>§ |
| Shortlist skills for a project | §skillman recommend -C <project> --brief§ |
| Preview and install | §skillman add -C <project> <name> --dry-run§, then without §--dry-run§ |
| Project status and CI check | §skillman status -C <project>§ · §skillman verify <project>§ |
| Upstream updates | §skillman library check§ · §skillman update -C <project> --dry-run§ |
| Import from GitHub | §skillman import <owner/repo> --list§ |
| New local skill | §skillman library new <name> -d "<what it does and when to use it>"§ |

Run §skillman help§ for every command, or §skillman§ for the terminal UI.
`, "§", "`")

var selfTools = []string{"skillman", "skill-add", "skill-search"}

// legacyMarkers identify files written by the Python skillman (<= 0.1).
func isLegacyReadme(text string) bool {
	return strings.Contains(text, "skillcore.py")
}

// installDirs lists where installers put the binary, PATH first.
func installDirs(home string) (path []string, extra []string) {
	seen := map[string]bool{}
	add := func(list *[]string, d string) {
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if !seen[d] {
			seen[d] = true
			*list = append(*list, d)
		}
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		add(&path, d)
	}
	add(&extra, filepath.Join(home, ".local", "bin"))
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		add(&extra, gobin)
	}
	add(&extra, filepath.Join(home, "go", "bin"))
	return path, extra
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func toolVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func selfChecks() []SelfCheck {
	var out []SelfCheck
	add := func(level, check, fix, format string, a ...any) {
		out = append(out, SelfCheck{level, check, fmt.Sprintf(format, a...), fix})
	}
	home, _ := os.UserHomeDir()
	exe, _ := os.Executable()
	exeReal, err := filepath.EvalSymlinks(exe)
	if err != nil {
		exeReal = exe
	}
	self := versionString()
	add("ok", "binary", "", "running %s (%s)", exeReal, self)

	// ── installed commands ──
	pathDirs, extraDirs := installDirs(home)
	reinstall := "rerun ./install.sh from a skillman checkout, or go install github.com/hoangisreal/skillman/cmd/skillman@main, then remove the stale entry"
	for _, name := range selfTools {
		effective := ""
		for i, dir := range append(append([]string{}, pathDirs...), extraDirs...) {
			onPath := i < len(pathDirs)
			p := filepath.Join(dir, exeName(name))
			info, err := os.Lstat(p)
			if err != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				target, _ := os.Readlink(p)
				if _, err := os.Stat(p); err != nil {
					add("error", "install", reinstall, "%s is a broken link to %s", p, target)
					continue
				}
			}
			if !onPath || effective != "" {
				continue
			}
			effective = p
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				real = p
			}
			if name == "skillman" && real != exeReal {
				if v := toolVersion(p); v != self {
					add("warn", "install", reinstall, "PATH runs %s (%s), not this binary %s (%s)", p, orUnknown(v), exeReal, self)
					continue
				}
			}
			add("ok", "install", "", "%s → %s", name, real)
		}
		if effective == "" {
			level := "info"
			if name == "skillman" {
				level = "warn"
			}
			add(level, "install", "add the directory that holds skillman to PATH, or "+reinstall, "%s is not on PATH", name)
		}
	}
	for _, dir := range append(pathDirs, extraDirs...) {
		p := filepath.Join(dir, "skillman.lock")
		if core.IsSymlink(p) && !core.Exists(p) {
			add("warn", "install", "remove it: rm "+p, "%s is a broken link left by the Python skillman", p)
		}
	}

	// ── library ──
	if !core.IsDir(core.SKILLS) {
		add("error", "library", "skillman init", "library not found at %s", core.LIB)
	} else {
		add("ok", "library", "", "library at %s (%d skills)", core.LIB, len(core.LibraryNames()))
		if sl := filepath.Join(core.LIB, "scripts"); core.IsSymlink(sl) && !core.Exists(sl) {
			add("warn", "library", "skillman init removes it", "%s is a broken link to the removed Python scripts", sl)
		}
		if isLegacyReadme(readOr(filepath.Join(core.LIB, "README.md"))) {
			add("warn", "library", "skillman init rewrites it (the old text stays in the library's git history)",
				"%s describes the Python skillcore.py commands", filepath.Join(core.LIB, "README.md"))
		}
		sk := filepath.Join(core.SKILLS, core.SelfSkill)
		shipped, _ := skillman.Assets.ReadFile("skills/skillman/SKILL.md")
		current := readOr(filepath.Join(sk, "SKILL.md"))
		switch {
		case current == "":
			add("warn", "library", "skillman init", "the skillman skill is missing from the library")
		case current != string(shipped) && strings.Contains(readOr(filepath.Join(sk, "SOURCE.json")), `"shipped_with": "skillman"`):
			add("warn", "library", "skillman init refreshes it",
				"%s/SKILL.md is not the version shipped with this binary, so agents that read it may run old commands", sk)
		case current != string(shipped):
			add("info", "library", "", "%s/SKILL.md was edited locally and is left as is", sk)
		}
		if core.IsDir(filepath.Join(core.LIB, ".git")) {
			if st := core.Git([]string{"status", "--porcelain"}, core.LIB, 30); st.Code == 0 {
				if st.Stdout != "" {
					changed := strings.Split(strings.TrimRight(st.Stdout, "\n"), "\n")
					example := strings.TrimSpace(changed[0])
					add("warn", "library", fmt.Sprintf("review, then: git -C %s add -A && git -C %s commit -m <message>", core.LIB, core.LIB),
						"%d uncommitted change(s) in the library, e.g. %s", len(changed), example)
				}
			}
		} else {
			add("warn", "library", "skillman init", "%s is not a git repository, so library changes have no history", core.LIB)
		}
	}

	// ── machine state ──
	if err := core.WritableDir(core.STATE); err != nil {
		add("warn", "state", "", "%s cannot be written (project registry, trash, settings): %v", core.STATE, err)
	} else {
		add("ok", "state", "", "%s is writable", core.STATE)
	}

	// ── the skillman skill for agents ──
	for _, root := range []string{filepath.Join(home, ".claude", "skills"), filepath.Join(home, ".agents", "skills")} {
		p := filepath.Join(root, core.SelfSkill)
		switch {
		case core.IsSymlink(p) && !core.Exists(p):
			target, _ := os.Readlink(p)
			add("error", "agents", "skillman agent-setup", "%s is a broken link to %s", p, target)
		case !core.Lexists(p):
			add("info", "agents", "skillman agent-setup", "%s is not set up, so agents outside a project do not know skillman", p)
		default:
			add("ok", "agents", "", "%s", p)
		}
	}
	return out
}

func orUnknown(s string) string {
	if s == "" {
		return "version unknown"
	}
	return s
}

func cmdDoctorSelf(a *Args) int {
	checks := selfChecks()
	bad := false
	for _, c := range checks {
		bad = bad || c.Level == "error"
	}
	if a.Has("json") {
		printJSON(map[string]any{"ok": !bad, "checks": checks})
	} else {
		icon := map[string]string{"ok": "✓", "info": "·", "warn": "!", "error": "✗"}
		for _, c := range checks {
			outf("%s %-8s %s\n", icon[c.Level], c.Check, c.Message)
			if c.Fix != "" {
				outf("           fix: %s\n", c.Fix)
			}
		}
	}
	if bad {
		return 1
	}
	return 0
}
