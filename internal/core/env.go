// Package core is the skill library engine: library index, packs, projects, trash, audit and verify.
// It keeps the on-disk formats of the original Python skillcore.py byte-compatible.
package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Paths and settings, resolved from the environment by Configure (called at init and by tests).
var (
	LIB, SKILLS, PACKS, STAGING, CACHE, REPO_CACHE, UPDATES, STATE string
	LegacyWork                                                     string
	GitBase                                                        string
	TokenBudget                                                    int
	GlobalSkillDirs                                                []string
)

var (
	KEBAB     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	PACK_NAME = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:/[a-z0-9]+(?:-[a-z0-9]+)*)?$`)
)

const (
	MaxLines = 500
	Manifest = "skill-manifest.json"
	// SelfSkill is the skill shipped with skillman; installing it globally is expected, so doctor does not
	// report it as shadowing.
	SelfSkill = "skillman"
)

var (
	ExcludeDirs  = set("hooks", "commands", ".claude-plugin", ".codex-plugin", "node_modules", ".git", "__pycache__")
	ExcludeFiles = set(".mcp.json", "plugin.json", "marketplace.json", ".DS_Store")
	HashIgnore   = set("__pycache__", ".DS_Store")
	ScriptExt    = set(".sh", ".bash", ".zsh", ".py", ".js", ".mjs", ".cjs", ".ts", ".rb", ".pl", ".ps1", ".php")
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Configure (re)reads SKILL_LIBRARY, SKILLMAN_STATE, SKILLCORE_GIT_BASE, SKILL_TOKEN_BUDGET.
func Configure() {
	LIB = expand(envOr("SKILL_LIBRARY", "~/skill-library"))
	if a, err := filepath.Abs(LIB); err == nil {
		LIB = a
	}
	SKILLS = filepath.Join(LIB, "skills")
	PACKS = filepath.Join(LIB, "packs")
	STAGING = filepath.Join(LIB, "staging")
	CACHE = filepath.Join(LIB, ".cache")
	REPO_CACHE = filepath.Join(CACHE, "repos")
	UPDATES = filepath.Join(CACHE, "updates")
	LegacyWork = filepath.Join(envOr("TMPDIR", os.TempDir()), "skill-verify")
	STATE = expand(envOr("SKILLMAN_STATE", "~/.local/state/skillman"))
	GitBase = envOr("SKILLCORE_GIT_BASE", "https://github.com/")
	TokenBudget = 2000
	if n, err := strconv.Atoi(os.Getenv("SKILL_TOKEN_BUDGET")); err == nil {
		TokenBudget = n
	}
	GlobalSkillDirs = nil
	for _, p := range []string{"~/.agents/skills", "~/.claude/skills", "~/.codex/skills", "~/.config/opencode/skills"} {
		GlobalSkillDirs = append(GlobalSkillDirs, expand(p))
	}
	resetCaches()
}

func init() { Configure() }

// SkillError is a user-facing failure (bad input, refused operation).
type SkillError struct{ Msg string }

func (e *SkillError) Error() string { return e.Msg }

func errf(format string, a ...any) error { return &SkillError{Msg: sprintf(format, a...)} }

// Result is one per-item outcome of a batch operation (install, update, ...).
type Result struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func res(name string, ok bool, format string, a ...any) Result {
	return Result{Name: name, OK: ok, Message: sprintf(format, a...)}
}
