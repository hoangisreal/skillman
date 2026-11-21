package core

import (
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

// trashSlot creates a fresh trash directory. project != "" allows the in-project fallback.
func trashSlot(kind, label, project string) (string, error) {
	stamp := fmt.Sprintf("%s-%d-%d", time.Now().Format("20060102-150405"), os.Getpid(),
		(time.Now().UnixNano()/1000+slotSeq.Add(1))%1_000_000)
	var base string
	switch {
	case stateWritable():
		base = filepath.Join(STATE, "trash")
	case project != "":
		base = filepath.Join(project, ".agents", ".skill-trash")
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
		bases = append(bases, filepath.Join(project, ".agents", ".skill-trash"))
	}
	var out []TrashEntry
	for _, b := range bases {
		ents := SortedDir(b)
		sort.Slice(ents, func(i, j int) bool { return ents[i].Name() > ents[j].Name() })
		for _, e := range ents {
			slot := filepath.Join(b, e.Name())
			m := LoadMap(filepath.Join(slot, "TRASH.json"))
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
	os.MkdirAll(filepath.Dir(origin), 0o755)
	if err := Move(payload[0], origin); err != nil {
		return "", err
	}
	kind, name := getStr(m, "kind"), getStr(m, "name")
	if kind == "skill" {
		proj := getStr(m, "project")
		if err := link(proj, name); err != nil {
			return "", err
		}
		if me, ok := m["manifest_entry"].(map[string]any); ok && me != nil {
			unlock, err := ProjectLock(proj)
			if err != nil {
				return "", err
			}
			man := ReadManifest(proj)
			man.Skills[name] = entryFromMap(me)
			err = WriteManifest(proj, man)
			unlock()
			if err != nil {
				return "", err
			}
		}
	}
	os.RemoveAll(slot)
	return fmt.Sprintf("restored %s %s → %s", kind, name, origin), nil
}

// EmptyTrash deletes the given slots (nil = all). Returns how many were removed.
func EmptyTrash(slots []string) int {
	if slots == nil {
		for _, e := range ListTrash("") {
			slots = append(slots, e.S("slot"))
		}
	}
	n := 0
	for _, s := range slots {
		if Exists(filepath.Join(s, "TRASH.json")) {
			os.RemoveAll(s)
			n++
		}
	}
	return n
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
