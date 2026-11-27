package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SkillOptions describe a new skill.
type SkillOptions struct {
	Description string
	Body        string // markdown after the frontmatter; "" uses the template
	Lang        string // template language: "en" (default) or "vi"
	NoCommit    bool   // library only: leave the change uncommitted
}

var skillTemplates = map[string]string{
	"en": `# %s

## When to use
- The situations in which an agent should load this skill.

## Steps
1. Short, concrete instructions. Put long reference material in files next to this one and link them.

## Checks
- How the agent verifies the job is done.
`,
	"vi": `# %s

## Khi nào dùng
- Các tình huống agent nên nạp skill này.

## Các bước
1. Hướng dẫn ngắn, cụ thể. Tài liệu tham khảo dài đặt trong file cạnh file này và liên kết tới.

## Kiểm tra
- Cách agent xác nhận công việc đã xong.
`,
}

var placeholderDesc = regexp.MustCompile(`(?i)^(x+|todo|tbd|test|placeholder|desc(ription)?|none|n/a|\.+|-+)$`)

// CheckDescription refuses descriptions that do not tell an agent when to use a skill.
func CheckDescription(desc string) error {
	d := collapseWS(desc)
	switch {
	case placeholderDesc.MatchString(d) || runeLen(d) < 20 || len(strings.Fields(d)) < 4:
		return errf("description %q is a placeholder: say what the skill does and when an agent should use it (at least 4 words)", d)
	case runeLen(d) > 1024:
		return errf("description is longer than 1024 characters")
	}
	return nil
}

// SkillFromFile reads a SKILL.md or plain Markdown file. A frontmatter
// description is returned for use when none was given; the body is the text
// after the frontmatter.
func SkillFromFile(path string) (description, body string, err error) {
	text, err := ReadText(path)
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(text, "---") {
		return "", text, nil
	}
	fm, msg := ParseFrontmatter(text)
	if fm == nil {
		return "", "", errf("%s: %s", path, msg)
	}
	if v, ok := fm["description"]; ok {
		description = collapseWS(pyStr(v))
	}
	parts := strings.SplitN(text, "---", 3)
	return description, strings.TrimLeft(parts[2], "\r\n"), nil
}

func renderSkill(name string, o SkillOptions) ([]byte, string, error) {
	desc := collapseWS(o.Description)
	if desc == "" {
		desc = fmt.Sprintf("TODO: what %s does and when an agent should use it.", name)
	} else if err := CheckDescription(desc); err != nil {
		return nil, "", err
	}
	lang := o.Lang
	if lang == "" {
		lang = "en"
	}
	tpl, ok := skillTemplates[lang]
	if !ok {
		return nil, "", errf("unknown template language %q (en, vi)", lang)
	}
	title := strings.ReplaceAll(name, "-", " ")
	title = strings.ToUpper(title[:1]) + title[1:]
	body := o.Body
	switch {
	case strings.TrimSpace(body) == "":
		body = fmt.Sprintf(tpl, title)
	case !strings.HasPrefix(strings.TrimSpace(body), "#"):
		body = "# " + title + "\n\n" + body
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return []byte("---\nname: " + name + "\ndescription: " + jsonQuote(desc) + "\n---\n\n" + strings.TrimLeft(body, "\n")), desc, nil
}

// NewSkill scaffolds a local skill in the library (kept for callers and tests).
func NewSkill(name, description string) (string, error) {
	d, _, err := NewLibrarySkill(name, SkillOptions{Description: description})
	return d, err
}

// NewLibrarySkill creates skills/<name> in the library, rebuilds the index and
// commits both unless NoCommit is set. A failed commit is returned as
// commitErr: the skill itself was created.
func NewLibrarySkill(name string, o SkillOptions) (dir string, commitErr, err error) {
	dir, err = newLibrarySkill(name, o)
	if err != nil || o.NoCommit || !IsDir(filepath.Join(LIB, ".git")) {
		return dir, nil, err
	}
	paths := []string{"skills/" + name}
	for _, f := range []string{"INDEX.json", "INDEX.md", "VALIDATION.md"} {
		if Exists(filepath.Join(LIB, f)) {
			paths = append(paths, f)
		}
	}
	return dir, commitLibrary("Add local skill "+name, paths), nil
}

func newLibrarySkill(name string, o SkillOptions) (string, error) {
	if err := CheckName(name); err != nil {
		return "", err
	}
	d := filepath.Join(SKILLS, name)
	if Lexists(d) {
		return "", errf("%s already exists in the library", name)
	}
	content, _, err := renderSkill(name, o)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	if err := AtomicWrite(filepath.Join(d, "SKILL.md"), content); err != nil {
		return "", err
	}
	src := NewOMap()
	src.Set("origin", "local")
	src.Set("repo", nil)
	src.Set("subpath", nil)
	src.Set("commit", nil)
	src.Set("created_at", Now())
	src.Set("tier", "local")
	if err := WriteJSON(filepath.Join(d, "SOURCE.json"), src); err != nil {
		return "", err
	}
	_, _, _, err = RebuildIndex(true)
	return d, err
}

// ProjectSource marks a manifest entry for a skill that lives only in its project.
const ProjectSource = "project"

func (e *Entry) ProjectOnly() bool { return e != nil && e.Source == ProjectSource }

// NewProjectSkill creates .agents/skills/<name> in project d, links it for
// Claude Code and records it as a project skill: never in the library, so
// search and recommend elsewhere do not see it, while verify still checks it.
func NewProjectSkill(d, name string, o SkillOptions) (string, error) {
	if err := CheckName(name); err != nil {
		return "", err
	}
	if strings.TrimSpace(o.Description) == "" {
		return "", errf("a project skill needs a description (-d \"what it does and when to use it\")")
	}
	content, _, err := renderSkill(name, o)
	if err != nil {
		return "", err
	}
	unlock, err := ProjectLock(d)
	if err != nil {
		return "", err
	}
	defer unlock()
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return "", err
	}
	dest := Dest(d, name)
	if Lexists(dest) || man.Skills[name] != nil {
		return "", errf("%s already exists in the project (to track an existing folder: skillman adopt --project %s)", dest, name)
	}
	copies := man.copyMode()
	if err := checkInstallTargets(d, name, nil, copies); err != nil {
		return "", err
	}
	if err := WritableDir(filepath.Dir(dest)); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	if err := AtomicWrite(filepath.Join(dest, "SKILL.md"), content); err != nil {
		os.RemoveAll(dest)
		return "", err
	}
	change, err := installFiles(d, name, "", false, copies, nil, "")
	if err != nil {
		os.RemoveAll(dest)
		return "", err
	}
	man.Skills[name] = projectEntry(dest, nil)
	if copies {
		man.top.Set("claude_mode", "copy")
	}
	if err := WriteManifest(d, man); err != nil {
		return "", errors.Join(err, change.rollback(), os.RemoveAll(dest))
	}
	change.finish()
	RegisterProject(d)
	return dest, nil
}

func projectEntry(dest string, prev *Entry) *Entry {
	e := &Entry{}
	if prev != nil {
		cp := *prev
		e = &cp
	}
	e.Source, e.UpstreamCommit, e.TreeHash, e.InstalledAt = ProjectSource, nil, TreeHash(dest), Now()
	e.Packs, e.Manual = []string{}, true
	return e
}

// AdoptProject tracks project folders as project skills: an untracked folder,
// or a tracked skill that should stop following the library.
func AdoptProject(d string, names []string) ([]Result, error) {
	if len(names) == 0 {
		return nil, errf("name the project skills to adopt")
	}
	for _, n := range names {
		if err := CheckName(n); err != nil {
			return nil, err
		}
	}
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	copies := man.copyMode()
	var out []Result
	var changes []installChange
	for _, name := range dedup(names) {
		dest := Dest(d, name)
		if !regularSkill(dest) {
			out = append(out, res(name, false, "%s: expected a real folder with a regular SKILL.md", dest))
			continue
		}
		if err := checkPortableSkill(name, dest); err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		prev := man.Skills[name]
		if prev.ProjectOnly() {
			out = append(out, res(name, true, "already a project skill"))
			continue
		}
		if !linkOK(d, name, copies, TreeHash) {
			change, err := installFiles(d, name, "", false, copies, prev, "")
			if err != nil {
				out = append(out, res(name, false, "%v", err))
				continue
			}
			changes = append(changes, change)
		}
		man.Skills[name] = projectEntry(dest, prev)
		msg := "tracked as a project skill"
		if lib := filepath.Join(SKILLS, name); IsFile(filepath.Join(lib, "SKILL.md")) {
			// The library copy is no longer used by this project; say how to drop it.
			if Git([]string{"ls-files", "--error-unmatch", "skills/" + name + "/SKILL.md"}, LIB, 10).Code == 0 {
				msg += sprintf("; it no longer follows the library, remove the library copy with: git -C %s rm -r skills/%s", LIB, name)
			} else {
				msg += sprintf("; it no longer follows the library, and the uncommitted library copy %s can be deleted", lib)
			}
		}
		out = append(out, res(name, true, "%s", msg))
	}
	if len(changes) == 0 && !slicesAnyOK(out) {
		return out, nil
	}
	if err := WriteManifest(d, man); err != nil {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		return out, err
	}
	for _, c := range changes {
		c.finish()
	}
	return out, nil
}

func slicesAnyOK(rs []Result) bool {
	for _, r := range rs {
		if r.OK && r.Message != "already a project skill" {
			return true
		}
	}
	return false
}
