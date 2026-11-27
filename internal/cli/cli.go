// Package cli implements every skillman command. All commands are non-interactive; read commands take
// --json so coding agents (Claude Code, Codex, OpenCode) can drive skillman as reliably as a person.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"github.com/hoangisreal/skillman/internal/core"
)

// Version is set by the build (-ldflags "-X .../cli.Version=...").
var Version = "0.2.0"

// versionString adds the Git revision of the build, so two builds of the
// same release can be told apart (doctor --self compares them).
func versionString() string {
	v := "skillman " + Version
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if len(rev) >= 7 {
		v += " (" + rev[:7]
		if dirty {
			v += ", modified"
		}
		v += ")"
	}
	return v
}

// TUI is installed by main to avoid an import cycle (cli <- tui).
var TUI func(project string) error

var stdout io.Writer = os.Stdout
var stderr io.Writer = os.Stderr

// ── args ──

type Args struct {
	Pos   []string
	vals  map[string][]string
	bools map[string]bool
}

func (a *Args) Has(k string) bool { return a.bools[k] || len(a.vals[k]) > 0 }

func (a *Args) Str(k, def string) string {
	if v := a.vals[k]; len(v) > 0 {
		return v[len(v)-1]
	}
	return def
}

// List returns every value of a repeatable flag; comma-separated values are split.
func (a *Args) List(k string) []string {
	var out []string
	for _, v := range a.vals[k] {
		for _, x := range strings.Split(v, ",") {
			if x = strings.TrimSpace(x); x != "" {
				out = append(out, x)
			}
		}
	}
	return out
}

func (a *Args) Int(k string, def int) int {
	if v := a.Str(k, ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// parse splits argv into positionals, value flags (named in valueFlags) and boolean flags.
// Flags may appear anywhere; "--" ends flag parsing; "--k=v" is accepted.
func parse(argv []string, valueFlags ...string) *Args {
	vf := map[string]bool{}
	for _, f := range valueFlags {
		vf[f] = true
	}
	a := &Args{vals: map[string][]string{}, bools: map[string]bool{}}
	for i := 0; i < len(argv); i++ {
		s := argv[i]
		if s == "--" {
			a.Pos = append(a.Pos, argv[i+1:]...)
			break
		}
		if len(s) < 2 || s[0] != '-' || isNumber(s) {
			a.Pos = append(a.Pos, s)
			continue
		}
		k := strings.TrimLeft(s, "-")
		if key, v, ok := strings.Cut(k, "="); ok {
			a.vals[key] = append(a.vals[key], v)
			continue
		}
		if vf[k] && i+1 < len(argv) {
			a.vals[k] = append(a.vals[k], argv[i+1])
			i++
			continue
		}
		a.bools[k] = true
	}
	return a
}

func isNumber(s string) bool { _, err := strconv.Atoi(s); return err == nil }

// ── output helpers ──

func outf(format string, a ...any) { fmt.Fprintf(stdout, format, a...) }
func errfln(format string, a ...any) {
	fmt.Fprintf(stderr, format+"\n", a...)
}

func printJSON(v any) {
	b, err := core.MarshalPy(v, 2)
	if err != nil {
		errfln("skillman: %v", err)
		return
	}
	stdout.Write(b)
}

// printResults prints ✓/✗ lines; returns 1 when any failed.
func printResults(rs []core.Result) int {
	rc := 0
	for _, r := range rs {
		if r.OK {
			outf("✓ %s: %s\n", r.Name, r.Message)
		} else {
			fmt.Fprintf(stderr, "✗ %s: %s\n", r.Name, r.Message)
			rc = 1
		}
	}
	return rc
}

func resultsOut(a *Args, rs []core.Result, err error) int {
	if err != nil {
		return fail(err)
	}
	if rs == nil {
		rs = []core.Result{}
	}
	if a.Has("json") {
		ok := true
		for _, r := range rs {
			ok = ok && r.OK
		}
		printJSON(map[string]any{"ok": ok, "results": rs})
		if ok {
			return 0
		}
		return 1
	}
	if len(rs) == 0 {
		outf("(nothing to do)\n")
	}
	return printResults(rs)
}

func fail(err error) int {
	var se *core.SkillError
	if errors.As(err, &se) {
		errfln("skillman: %s", se.Msg)
	} else {
		if !strings.Contains(err.Error(), core.SandboxHint) {
			err = core.ExplainFSError(err)
		}
		errfln("skillman: %v", err)
	}
	return 1
}

func usage(msg string) int {
	errfln("usage: %s", msg)
	return 2
}

// projectDir: -C, else the first positional (when it is a directory), else ".".
func projectDir(a *Args, posIsDir bool) (string, error) {
	d := a.Str("C", "")
	if d == "" && posIsDir && len(a.Pos) > 0 {
		d = a.Pos[0]
	}
	return core.ResolveProject(d, false)
}

// ── dispatch ──

type command struct {
	name, args, help string
	values           []string
	run              func(a *Args) int
	hidden           bool
}

var commands []command

func reg(name, args, help string, values []string, run func(a *Args) int) {
	commands = append(commands, command{name: name, args: args, help: help, values: values, run: run})
}

func alias(name, target string, prefix ...string) {
	commands = append(commands, command{name: name, hidden: true, run: func(a *Args) int {
		c := find(target)
		a2 := parse(append(append([]string{}, prefix...), a.raw()...), c.values...)
		return c.run(a2)
	}})
}

// raw reconstructs argv (used by aliases that re-parse with another flag set).
func (a *Args) raw() []string {
	out := append([]string{}, a.Pos...)
	keys := make([]string, 0)
	for k := range a.bools {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, "--"+k)
	}
	for k, vs := range a.vals {
		for _, v := range vs {
			out = append(out, "--"+k+"="+v)
		}
	}
	return out
}

func find(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// Run executes argv[1:] as invoked through prog (argv[0]); returns the exit code.
func Run(prog string, argv []string) int {
	switch filepath.Base(prog) {
	case "skill-add":
		return skillAdd(argv)
	case "skill-search":
		if len(argv) == 0 {
			return usage("skill-search <term>...")
		}
		return cmdSearch(parse(append(argv, "--legacy"), searchValues...))
	}
	if len(argv) == 0 {
		return runTUI("")
	}
	name := argv[0]
	if name == "-h" || name == "--help" || name == "help" {
		if len(argv) > 1 {
			return helpFor(argv[1])
		}
		printHelp()
		return 0
	}
	if name == "--version" || name == "version" {
		outf("%s\n", versionString())
		return 0
	}
	c := find(name)
	if c == nil {
		if core.IsDir(name) && len(argv) == 1 {
			return runTUI(name)
		}
		errfln("skillman: unknown command %q (see: skillman help)", name)
		return 2
	}
	rest := argv[1:]
	if len(rest) > 0 && (rest[0] == "-h" || rest[0] == "--help") && !c.hidden {
		return helpFor(name)
	}
	return c.run(parse(rest, c.values...))
}

func runTUI(dir string) int {
	if TUI == nil {
		return fail(fmt.Errorf("TUI not available in this build"))
	}
	p, err := core.ResolveProject(orDot(dir), dir == "")
	if err != nil {
		return fail(err)
	}
	if err := TUI(p); err != nil {
		return fail(err)
	}
	return 0
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}

func helpFor(name string) int {
	c := find(name)
	if c == nil || c.hidden {
		errfln("skillman: unknown command %q", name)
		return 2
	}
	outf("skillman %s %s\n\n%s\n", c.name, c.args, c.help)
	return 0
}

func printHelp() {
	outf(`skillman %s — cold skill library manager for coding agents (Claude Code, Codex, OpenCode)

Usage: skillman [project-dir]          open the TUI (default: git root of the current dir)
       skillman <command> [args]       every command is non-interactive; read commands take --json

Library: %s   (set SKILL_LIBRARY to change)

Commands:
`, Version, core.LIB)
	for _, c := range commands {
		if c.hidden {
			continue
		}
		head := c.name
		if c.args != "" {
			head += " " + c.args
		}
		first := strings.SplitN(c.help, "\n", 2)[0]
		if len(head) > 44 {
			outf("  %s\n  %-44s %s\n", head, "", first)
		} else {
			outf("  %-44s %s\n", head, first)
		}
	}
	outf(`
Aliases kept from skillcore.py: install, uninstall, packs, expand, validate-packs, update-library,
rebuild-index, rollback, lib-pin, lib-unpin, new. skill-add and skill-search work as before.
Details: skillman help <command>
`)
}
