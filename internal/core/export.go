package core

import (
	"os"
	"path/filepath"
	"strings"
)

// ExportMarkdown lists skill files present in the project's primary skill directory.
func ExportMarkdown(project string) (string, error) {
	root := filepath.Join(project, ".agents", "skills")
	if IsSymlink(filepath.Dir(root)) || IsSymlink(root) {
		return "", errf("project skill directory is a symlink: %s", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var out strings.Builder
	out.WriteString("# Project skills\n\n")
	escape := strings.NewReplacer("\\", "\\\\", "`", "\\`", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "&", "&amp;")
	for _, entry := range entries {
		name := entry.Name()
		dir := Dest(project, name)
		if !KEBAB.MatchString(name) || !entry.IsDir() || !regularSkill(dir) {
			continue
		}
		out.WriteString("- **" + name + "** — `.agents/skills/" + name + "/SKILL.md`")
		if desc, ok := SkillMeta(dir)["description"].(string); ok && strings.TrimSpace(desc) != "" {
			desc = collapseWS(desc)
			if chars := []rune(desc); len(chars) > 180 {
				desc = strings.TrimSpace(string(chars[:179])) + "…"
			}
			out.WriteString(" — " + escape.Replace(desc))
		}
		out.WriteByte('\n')
	}
	return out.String(), nil
}

// WriteSkillExport uses exclusive creation unless replacement was authorized.
func WriteSkillExport(path, markdown string, overwrite bool) error {
	if overwrite {
		return AtomicWrite(path, []byte(markdown))
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(markdown)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}
