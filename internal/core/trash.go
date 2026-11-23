package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"
)

func stateWritable() bool {
	if err := os.MkdirAll(STATE, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(STATE, ".probe")
	if err := os.WriteFile(probe, nil, 0o644); err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

var slotSeq atomic.Int64

func projectTrash(project string) (string, error) {
	agents := filepath.Join(project, ".agents")
	dir := filepath.Join(agents, ".skill-trash")
	if IsSymlink(agents) || IsSymlink(dir) {
		return "", errf("project trash path contains a symlink: %s", dir)
	}
	return dir, nil
}

// trashSlot creates a fresh trash directory. project != "" allows the in-project fallback.
func trashSlot(kind, label, project string) (string, error) {
	stamp := fmt.Sprintf("%s-%d-%d", time.Now().Format("20060102-150405"), os.Getpid(),
		(time.Now().UnixNano()/1000+slotSeq.Add(1))%1_000_000)
	var base string
	switch {
	case stateWritable():
		base = filepath.Join(STATE, "trash")
	case project != "":
		var err error
		base, err = projectTrash(project)
		if err != nil {
			return "", err
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", err
		}
		if gi := filepath.Join(base, ".gitignore"); !Exists(gi) {
			os.WriteFile(gi, []byte("*\n"), 0o644)
		}
	default:
		base = filepath.Join(CACHE, "trash")
	}
	d := filepath.Join(base, fmt.Sprintf("%s-%s-%s", stamp, kind, label))
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func writeTrashMeta(slot string, meta map[string]any) {
	WriteJSON(filepath.Join(slot, "TRASH.json"), meta)
}

// TrashEntry is a TRASH.json plus where it lives.
type TrashEntry map[string]any

func (e TrashEntry) S(k string) string { return getStr(e, k) }

// ListTrash lists trash entries, newest first. project adds its in-project fallback trash.
func ListTrash(project string) []TrashEntry {
	bases := []string{filepath.Join(STATE, "trash"), filepath.Join(CACHE, "trash")}
	if project != "" {
		if dir, err := projectTrash(project); err == nil {
			bases = append(bases, dir)
		}
	}
	var out []TrashEntry
	for _, b := range bases {
		ents := SortedDir(b)
		sort.Slice(ents, func(i, j int) bool { return ents[i].Name() > ents[j].Name() })
		for _, e := range ents {
			if !e.IsDir() {
				continue
			}
			slot := filepath.Join(b, e.Name())
			meta := filepath.Join(slot, "TRASH.json")
			if IsSymlink(meta) {
				continue
			}
			m := LoadMap(meta)
			if len(m) == 0 {
				continue
			}
			m["slot"], m["id"] = slot, e.Name()
			out = append(out, TrashEntry(m))
		}
	}
	return out
}

func RestoreTrash(slot string) (string, error) {
	m := LoadMap(filepath.Join(slot, "TRASH.json"))
	if len(m) == 0 {
		return "", errf("not a trash entry: %s", slot)
	}
	origin := getStr(m, "origin")
	if Lexists(origin) {
		return "", errf("cannot restore, path exists: %s", origin)
	}
	var payload []string
	for _, e := range SortedDir(slot) {
		if e.Name() != "TRASH.json" {
			payload = append(payload, filepath.Join(slot, e.Name()))
		}
	}
	if len(payload) != 1 {
		return "", errf("corrupt trash entry")
	}
	kind, name := getStr(m, "kind"), getStr(m, "name")
	proj := getStr(m, "project")
	if base := filepath.Dir(slot); filepath.Base(base) == ".skill-trash" {
		project := filepath.Dir(filepath.Dir(base))
		if kind != "skill" || filepath.Clean(proj) != project {
			return "", errf("trash entry does not belong to this project")
		}
	}
	var man *ManifestData
	switch kind {
	case "pack":
		expected, err := PackPath(name)
		if err != nil || filepath.Clean(origin) != expected {
			return "", errf("invalid pack destination")
		}
	case "skill":
		if err := CheckName(name); err != nil {
			return "", err
		}
		if filepath.Clean(origin) != Dest(proj, name) {
			return "", errf("invalid skill destination")
		}
		unlock, err := ProjectLock(proj)
		if err != nil {
			return "", err
		}
		defer unlock()
		man, _, err = LoadProjectManifest(proj)
		if err != nil {
			return "", err
		}
		if Lexists(origin) || Lexists(LinkP(proj, name)) {
			return "", errf("restore destination occupied")
		}
	default:
		return "", errf("unknown trash entry type: %s", kind)
	}
	if err := os.MkdirAll(filepath.Dir(origin), 0755); err != nil {
		return "", err
	}
	if err := Move(payload[0], origin); err != nil {
		return "", err
	}
	if kind == "skill" {
		if err := link(proj, name, man.copyMode()); err != nil {
			return "", errors.Join(err, Move(origin, payload[0]))
		}
		if me, ok := m["manifest_entry"].(map[string]any); ok && me != nil {
			entry := entryFromMap(me)
			man.Skills[name] = entry
			for _, pack := range entry.Packs {
				if !man.Packs.Has(pack) {
					man.Packs.Set(pack, map[string]string{"installed_at": entry.InstalledAt})
				}
			}
			if err := WriteManifest(proj, man); err != nil {
				return "", errors.Join(err, os.RemoveAll(LinkP(proj, name)), Move(origin, payload[0]))
			}
		}
	}
	os.RemoveAll(slot)
	return fmt.Sprintf("restored %s %s → %s", kind, name, origin), nil
}

// EmptyTrash deletes the given slots (nil = all). It reports how many were removed and any failures.
func EmptyTrash(slots []string) (int, error) {
	if slots == nil {
		for _, e := range ListTrash("") {
			slots = append(slots, e.S("slot"))
		}
	}
	n := 0
	var problems []error
	for _, s := range slots {
		if Exists(filepath.Join(s, "TRASH.json")) {
			if err := os.RemoveAll(s); err != nil {
				problems = append(problems, err)
			} else {
				n++
			}
		}
	}
	return n, errors.Join(problems...)
}

// ── project registry ──

func RegisterProject(d string) {
	unlock, err := DirLock(STATE)
	if err != nil {
		return // read-only state dir: registry is best-effort
	}
	defer unlock()
	var reg struct {
		Projects []string `json:"projects"`
	}
	LoadJSON(filepath.Join(STATE, "projects.json"), &reg)
	if contains(reg.Projects, d) {
		return
	}
	reg.Projects = append(reg.Projects, d)
	sort.Strings(reg.Projects)
	WriteJSON(filepath.Join(STATE, "projects.json"), map[string]any{"projects": reg.Projects})
}

func RegisteredProjects() []string {
	var reg struct {
		Projects []string `json:"projects"`
	}
	LoadJSON(filepath.Join(STATE, "projects.json"), &reg)
	var out []string
	for _, p := range reg.Projects {
		if Exists(filepath.Join(p, ".agents", Manifest)) {
			out = append(out, p)
		}
	}
	return out
}
