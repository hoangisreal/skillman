package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// OMap is a JSON object that keeps key order (Python dicts do), so rewriting files does not reorder them.
type OMap struct {
	Keys []string
	Vals map[string]json.RawMessage
}

func NewOMap() *OMap { return &OMap{Vals: map[string]json.RawMessage{}} }

func (o *OMap) Has(k string) bool { _, ok := o.Vals[k]; return ok }

func (o *OMap) Set(k string, v any) {
	b := marshalNoEsc(v)
	if !o.Has(k) {
		o.Keys = append(o.Keys, k)
	}
	o.Vals[k] = b
}

func (o *OMap) Delete(k string) {
	if !o.Has(k) {
		return
	}
	delete(o.Vals, k)
	for i, x := range o.Keys {
		if x == k {
			o.Keys = append(o.Keys[:i], o.Keys[i+1:]...)
			break
		}
	}
}

func (o *OMap) Get(k string, v any) bool {
	b, ok := o.Vals[k]
	return ok && json.Unmarshal(b, v) == nil
}

func (o *OMap) Len() int { return len(o.Keys) }

func (o *OMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.Keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(marshalNoEsc(k))
		buf.WriteByte(':')
		buf.Write(o.Vals[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (o *OMap) UnmarshalJSON(b []byte) error {
	o.Keys, o.Vals = nil, map[string]json.RawMessage{}
	dec := json.NewDecoder(bytes.NewReader(b))
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return &json.UnmarshalTypeError{Value: "non-object"}
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		k, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if !o.Has(k) {
			o.Keys = append(o.Keys, k)
		}
		o.Vals[k] = raw
	}
	return nil
}

// marshalNoEsc = json.Marshal without HTML escaping (Python json.dumps never escapes <, >, &).
func marshalNoEsc(v any) []byte {
	b, err := MarshalPy(v, 0)
	if err != nil {
		return []byte("null")
	}
	return bytes.TrimSuffix(b, []byte("\n"))
}

// LoadOMap reads a JSON object preserving order (empty on error).
func LoadOMap(p string) *OMap {
	o := NewOMap()
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, o) != nil {
		return NewOMap()
	}
	return o
}

// Entry is one skill record in .agents/skill-manifest.json.
type Entry struct {
	Source         string
	UpstreamCommit any // string or null
	TreeHash       string
	InstalledAt    string
	Packs          []string
	Manual         bool
	Pinned         bool
	extra          *OMap // unknown keys, preserved
}

var entryKeys = []string{"source", "upstream_commit", "tree_hash", "installed_at", "packs", "manual", "pinned"}

func (e *Entry) MarshalJSON() ([]byte, error) {
	o := NewOMap()
	if e.extra != nil {
		for _, k := range e.extra.Keys {
			o.Keys = append(o.Keys, k)
			o.Vals[k] = e.extra.Vals[k]
		}
	}
	packs := e.Packs
	if packs == nil {
		packs = []string{}
	}
	o.Set("source", e.Source)
	o.Set("upstream_commit", e.UpstreamCommit)
	o.Set("tree_hash", e.TreeHash)
	o.Set("installed_at", e.InstalledAt)
	o.Set("packs", packs)
	o.Set("manual", e.Manual)
	o.Set("pinned", e.Pinned)
	return o.MarshalJSON()
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	o := NewOMap()
	if err := o.UnmarshalJSON(b); err != nil {
		return err
	}
	var commit *string
	fields := []any{&e.Source, &commit, &e.TreeHash, &e.InstalledAt, &e.Packs, &e.Manual, &e.Pinned}
	for i, k := range entryKeys {
		if raw, ok := o.Vals[k]; ok {
			if bytes.Equal(raw, []byte("null")) && k != "upstream_commit" && k != "packs" {
				return errf("invalid %s in skill entry", k)
			}
			if err := json.Unmarshal(raw, fields[i]); err != nil {
				return errf("invalid %s in skill entry", k)
			}
		}
	}
	if commit != nil {
		e.UpstreamCommit = *commit
	}
	// keep the original key order (known keys are re-set in place by MarshalJSON)
	e.extra = o
	return nil
}

func entryFromMap(m map[string]any) *Entry {
	b, _ := json.Marshal(m)
	var e Entry
	json.Unmarshal(b, &e)
	return &e
}

func (e *Entry) AsMap() map[string]any {
	b, _ := e.MarshalJSON()
	m := map[string]any{}
	json.Unmarshal(b, &m)
	return m
}

func (e *Entry) HasPack(p string) bool { return contains(e.Packs, p) }

// ManifestData is .agents/skill-manifest.json.
type ManifestData struct {
	Skills map[string]*Entry
	Packs  *OMap // pack -> {"installed_at": ...}
	top    *OMap
}

func ReadManifest(d string) *ManifestData {
	m, _, err := LoadProjectManifest(d)
	if err != nil {
		return emptyManifest(false)
	}
	return m
}

func emptyManifest(newProject bool) *ManifestData {
	top := NewOMap()
	top.Set("version", 1)
	if newProject && runtime.GOOS == "windows" {
		top.Set("claude_mode", "copy")
	}
	return &ManifestData{Skills: map[string]*Entry{}, Packs: NewOMap(), top: top}
}

// LoadProjectManifest distinguishes a new project from a damaged manifest.
func LoadProjectManifest(d string) (*ManifestData, bool, error) {
	file := filepath.Join(d, ".agents", Manifest)
	invalid := func(reason any) (*ManifestData, bool, error) {
		return nil, false, errf("invalid manifest %s: %v", file, reason)
	}
	if IsSymlink(filepath.Dir(file)) {
		return invalid("manifest directory is a symlink")
	}
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return emptyManifest(true), false, nil
	}
	if err != nil {
		return invalid(err)
	}
	if !info.Mode().IsRegular() {
		return invalid("expected a regular file")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return invalid(err)
	}
	top := NewOMap()
	if err := json.Unmarshal(b, top); err != nil {
		return invalid(err)
	}
	var version int
	if top.Has("version") && (!top.Get("version", &version) || version != 1) {
		return invalid("expected manifest version 1")
	}
	if !top.Has("version") {
		top.Set("version", 1)
	}
	var mode string
	if top.Has("claude_mode") && (!top.Get("claude_mode", &mode) || (mode != "symlink" && mode != "copy")) {
		return invalid("invalid Claude mode")
	}
	m := &ManifestData{Packs: NewOMap(), top: top}
	if err := json.Unmarshal(top.Vals["skills"], &m.Skills); err != nil {
		return invalid(err)
	}
	if m.Skills == nil {
		return invalid("skills must be an object")
	}
	for _, name := range m.SkillNames() {
		e := m.Skills[name]
		if !KEBAB.MatchString(name) || e == nil {
			return invalid("invalid skill entry " + name)
		}
		if _, err := hex.DecodeString(e.TreeHash); len(e.TreeHash) != 16 || err != nil {
			return invalid("invalid tree_hash for " + name)
		}
	}
	if top.Has("packs") && !top.Get("packs", m.Packs) {
		return invalid("packs must be an object")
	}
	return m, true, nil
}

func (m *ManifestData) copyMode() bool {
	var mode string
	return m.top.Get("claude_mode", &mode) && mode == "copy"
}

func (m *ManifestData) SkillNames() []string {
	out := make([]string, 0, len(m.Skills))
	for k := range m.Skills {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (m *ManifestData) PackNames() []string { return append([]string{}, m.Packs.Keys...) }

func WriteManifest(d string, m *ManifestData) error {
	top := m.top
	if top == nil {
		top = NewOMap()
		top.Set("version", 1)
	}
	if !top.Has("skills") {
		top.Set("skills", map[string]any{})
	}
	top.Set("skills", m.Skills) // encoding/json sorts map keys = Python dict(sorted(...))
	if !top.Has("packs") {
		top.Set("packs", map[string]any{})
	}
	top.Set("packs", m.Packs)
	// Every mode needs -text: Git checkouts that convert line endings would
	// otherwise change the bytes that the manifest hashes.
	var added []string
	if err := CheckProjectDirs(d); err != nil {
		return err
	}
	for _, dir := range []string{".agents/skills", ".claude/skills"} {
		p := filepath.Join(d, dir, ".gitattributes")
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			removeAddedAttributes(added)
			return err
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			removeAddedAttributes(added)
			return err
		}
		added = append(added, p)
		_, err = f.WriteString("# Keep Skillman manifest hashes stable across Git checkouts.\n* -text\n")
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			removeAddedAttributes(added)
			return err
		}
	}
	err := WriteJSON(filepath.Join(d, ".agents", Manifest), top)
	if err != nil {
		removeAddedAttributes(added)
	}
	return err
}

func removeAddedAttributes(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}
