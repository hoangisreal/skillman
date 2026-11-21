package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	o.Get("source", &e.Source)
	o.Get("upstream_commit", &e.UpstreamCommit)
	o.Get("tree_hash", &e.TreeHash)
	o.Get("installed_at", &e.InstalledAt)
	o.Get("packs", &e.Packs)
	o.Get("manual", &e.Manual)
	o.Get("pinned", &e.Pinned)
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
	top := LoadOMap(filepath.Join(d, ".agents", Manifest))
	m := &ManifestData{Skills: map[string]*Entry{}, Packs: NewOMap(), top: top}
	if !top.Has("version") {
		top.Set("version", 1)
	}
	var skills map[string]*Entry
	if top.Get("skills", &skills) && skills != nil {
		m.Skills = skills
	}
	var packs OMap
	if top.Get("packs", &packs) && packs.Vals != nil {
		m.Packs = &packs
	}
	return m
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
	return WriteJSON(filepath.Join(d, ".agents", Manifest), top)
}
