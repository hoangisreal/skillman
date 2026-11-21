#!/usr/bin/env python3
"""skillcore — shared logic for the cold skill library (packs, projects, upstream updates).

Used by: skill-add (wrapper), skillman (TUI), validate-library.py, import-skills.py,
verify-repos.py, tick-candidates.py. Stdlib + PyYAML only. Run `skillcore.py -h` for the CLI.

Env overrides (mainly for tests):
  SKILL_LIBRARY   library root (default ~/skill-library)
  SKILLMAN_STATE  state dir for trash + project registry (default ~/.local/state/skillman)
  SKILLCORE_GIT_BASE  upstream URL prefix (default https://github.com/)
  SKILL_TOKEN_BUDGET  per-project skill-description token budget (default 2000)
"""
from __future__ import annotations

import argparse
import contextlib
import difflib
import fcntl
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

import yaml

# ───────────────────────────── paths ─────────────────────────────
LIB = Path(os.environ.get("SKILL_LIBRARY") or "~/skill-library").expanduser()
TOOL_ROOT = Path(__file__).resolve().parent.parent  # skillman checkout (scripts/, bin/, packs/)
SKILLS = LIB / "skills"
PACKS = LIB / "packs"
STAGING = LIB / "staging"
CACHE = LIB / ".cache"
REPO_CACHE = CACHE / "repos"
UPDATES = CACHE / "updates"
LEGACY_WORK = Path(os.environ.get("TMPDIR") or tempfile.gettempdir()) / "skill-verify"
STATE = Path(os.environ.get("SKILLMAN_STATE") or "~/.local/state/skillman").expanduser()
GIT_BASE = os.environ.get("SKILLCORE_GIT_BASE", "https://github.com/")
TOKEN_BUDGET = int(os.environ.get("SKILL_TOKEN_BUDGET", "2000"))
GLOBAL_SKILL_DIRS = [Path(p).expanduser() for p in (
    "~/.agents/skills", "~/.claude/skills", "~/.codex/skills", "~/.config/opencode/skills")]

KEBAB = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
PACK_NAME = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*(?:/[a-z0-9]+(?:-[a-z0-9]+)*)?$")
MAX_LINES = 500
EXCLUDE_DIRS = {"hooks", "commands", ".claude-plugin", ".codex-plugin", "node_modules", ".git", "__pycache__"}
EXCLUDE_FILES = {".mcp.json", "plugin.json", "marketplace.json", ".DS_Store"}
HASH_IGNORE = {"__pycache__", ".DS_Store"}
SCRIPT_EXT = {".sh", ".bash", ".zsh", ".py", ".js", ".mjs", ".cjs", ".ts", ".rb", ".pl", ".ps1", ".php"}
MANIFEST = "skill-manifest.json"


class SkillError(Exception):
    pass


def now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%S%z")


# ───────────────────────────── io helpers ─────────────────────────────
def load_json(p: Path, default=None):
    try:
        return json.loads(Path(p).read_text())
    except Exception:
        return {} if default is None else default


def atomic_write(p: Path, text: str) -> None:
    p = Path(p)
    p.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=f".{p.name}.", dir=p.parent)
    try:
        with os.fdopen(fd, "w") as f:
            f.write(text)
        os.replace(tmp, p)
    except BaseException:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(tmp)
        raise


def write_json(p: Path, data) -> None:
    atomic_write(p, json.dumps(data, indent=2, ensure_ascii=False) + "\n")


@contextlib.contextmanager
def dir_lock(d: Path):
    """Exclusive flock on a directory (no lock file left behind)."""
    d = Path(d)
    d.mkdir(parents=True, exist_ok=True)
    fd = os.open(d, os.O_RDONLY)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX)
        yield
    finally:
        fcntl.flock(fd, fcntl.LOCK_UN)
        os.close(fd)


def git(args, cwd=None, timeout=300, env=None):
    return subprocess.run(["git", *args], cwd=cwd, capture_output=True, text=True, timeout=timeout,
                          env={**os.environ, "GIT_TERMINAL_PROMPT": "0", **(env or {})})


def tree_hash(d: Path) -> str:
    """Content hash of a directory: relative paths + file bytes (+ symlink targets)."""
    h = hashlib.sha256()
    d = Path(d)
    for p in sorted(d.rglob("*")):
        rel = p.relative_to(d)
        if set(rel.parts) & HASH_IGNORE:
            continue
        if p.is_symlink():
            h.update(b"L" + str(rel).encode() + b"\0" + os.readlink(p).encode() + b"\0")
        elif p.is_file():
            h.update(b"F" + str(rel).encode() + b"\0")
            h.update(hashlib.sha256(p.read_bytes()).digest())
    return h.hexdigest()[:16]


# ───────────────────────────── skill parsing / validation ─────────────────────────────
SUSPICIOUS = [
    (r"(curl|wget)[^\n|]*\|\s*(sudo\s+)?(ba|z)?sh\b", "pipe download to shell"),
    (r"base64\s+(-d|--decode)[^\n]*\|\s*(ba|z)?sh", "base64 decode piped to shell"),
    (r"eval\s*\(\s*(atob|base64|Buffer\.from)", "eval of encoded payload"),
    (r"(cat|read|open)[^\n]{0,40}(~|\$HOME)/\.(ssh|aws|kube|netrc)", "reads credential dir"),
    (r"(cat|source|read|open)\s*\(?['\"]?[^\s'\"]*\.env\b(?!\.(example|template|sample))", "reads .env"),
    (r"ignore (all )?(previous|prior) instructions", "prompt-injection phrase"),
    (r"(nc|ncat|socat)\s+-[a-z]*e\b", "reverse shell"),
]
CATEGORIES = {
    "web": r"\b(react|next\.?js|vue|nuxt|svelte|vite|tailwind|css|html|frontend|web|unocss|pinia|shadcn|astro|remix|tanstack)\b",
    "mobile": r"\b(expo|react native|flutter|dart|swift|swiftui|ios|android|kotlin|compose)\b",
    "backend": r"\b(api|rest|graphql|grpc|protobuf|spring|quarkus|django|fastapi|flask|nestjs|express|hono|laravel|rails|dotnet|\.net|asp\.net|server|workers|durable objects|nitro)\b",
    "database": r"\b(sql|postgres|postgresql|mysql|sqlite|supabase|redis|mongo|mongodb|duckdb|neo4j|clickhouse|prisma|ef core|database|orm)\b",
    "data": r"\b(pandas|polars|spark|dbt|airflow|dagster|kafka|etl|pipeline|dataset|spreadsheet|excel|csv|parquet)\b",
    "ml-ai": r"\b(ml|machine learning|pytorch|torch|tensorflow|hugging ?face|transformers|llm|model|training|fine-?tun|unsloth|scikit|gradio|mlops|eval)\b",
    "cloud-devops": r"\b(docker|dockerfile|kubernetes|k8s|terraform|helm|ci|github actions|aws|gcp|azure|cloudflare|deploy|observability|opentelemetry)\b",
    "security": r"\b(security|owasp|vulnerab|crypto|auth|oauth|secret|hardening|audit|variant analysis|threat)\b",
    "systems": r"\b(c\+\+|rust|go|golang|llvm|linux|kernel|embedded|reverse.engineer|binary|assembly|ebpf)\b",
    "testing": r"\b(test|tests|testing|tdd|vitest|jest|pytest|playwright|property-based|e2e|qa|verification)\b",
    "design": r"\b(design|ui|ux|typography|layout|visual|animation|figma|slides?|presentation)\b",
    "docs-research": r"\b(paper|citation|literature|research|writing|documentation|docs|prose|pdf|docx|readme)\b",
    "workflow": r"\b(commit|pr|review|plan|planning|refactor|debug|skill|workflow|ci fix)\b",
}


def parse_frontmatter(text: str):
    """-> (fm dict | None, error | None)"""
    if not text.startswith("---"):
        return None, "frontmatter must start on line 1 with ---"
    parts = text.split("---", 2)
    if len(parts) < 3:
        return None, "unterminated frontmatter"
    try:
        fm = yaml.safe_load(parts[1]) or {}
    except yaml.YAMLError as e:
        return None, f"invalid YAML: {str(e).splitlines()[0]}"
    if not isinstance(fm, dict):
        return None, "frontmatter is not a mapping"
    return fm, None


def skill_meta(d: Path) -> dict:
    try:
        fm, _ = parse_frontmatter((Path(d) / "SKILL.md").read_text(encoding="utf-8", errors="replace"))
    except OSError:
        fm = None
    return fm or {}


def _links(text: str):
    for m in re.finditer(r"\]\(([^)\s#]+)(?:#[^)]*)?\)", text):
        yield m.group(1)
    for m in re.finditer(r"`((?:references|scripts|assets)/[^`\s*]+)`", text):
        yield m.group(1)


_LOCK = _PROV = None


def source_of(d: Path) -> str:
    global _LOCK, _PROV
    if _LOCK is None:
        _LOCK = load_json(LIB / "provenance" / ".skill-lock.json").get("skills", {})
        _PROV = load_json(LIB / "provenance" / "skill-provenance.json").get("added", {})
    d = Path(d)
    s = load_json(d / "SOURCE.json")
    repo = s.get("repo") or s.get("repository")
    if repo:
        return repo
    if d.name in _LOCK:
        return _LOCK[d.name].get("source", "unknown")
    if d.name in _PROV:
        return _PROV[d.name].get("repository", "unknown")
    if s.get("origin") in ("local", "local-migrated"):
        return "local"
    return "unknown"


def check_skill(d: Path, name: str | None = None):
    """Validate one skill dir. -> (errors[str], warnings[str], index_row | None)"""
    d = Path(d)
    name = name or d.name
    errors, warns = [], []
    md = d / "SKILL.md"
    if not md.exists():
        return ["missing SKILL.md"], [], None
    text = md.read_text(encoding="utf-8", errors="replace")
    fm, err = parse_frontmatter(text)
    if err:
        errors.append(err)
        fm = {}
    fname, desc = fm.get("name"), fm.get("description")
    if not KEBAB.match(name):
        errors.append("directory name is not kebab-case")
    if fname != name:
        errors.append(f"frontmatter name {fname!r} != directory")
    if not isinstance(desc, str) or not desc.strip():
        errors.append("missing description")
        desc = ""
    elif len(desc) > 1024:
        warns.append(f"description {len(desc)} chars > 1024")
    n_lines = text.count("\n") + 1
    if n_lines > MAX_LINES:
        warns.append(f"SKILL.md has {n_lines} lines > {MAX_LINES}")
    for target in sorted(set(_links(text))):
        if re.match(r"^[a-z]+:|^/|^\$|^~", target) or "{" in target or "<" in target:
            continue
        if not (d / target).exists():
            warns.append(f"broken link: {target}")
    if "hooks" in fm:
        warns.append("REVIEW: frontmatter registers hooks")
    at = fm.get("allowed-tools")
    at_s = " ".join(at) if isinstance(at, list) else str(at or "")
    if re.search(r"(^|[\s,])Bash(\(\*\))?($|[\s,])", at_s):
        warns.append(f"REVIEW: broad allowed-tools: {at_s}")
    for f in [md, *d.rglob("scripts/*")]:
        if not f.is_file() or f.stat().st_size > 2_000_000:
            continue
        body = f.read_text(encoding="utf-8", errors="ignore")
        for rx, label in SUSPICIOUS:
            if re.search(rx, body, re.I):
                warns.append(f"REVIEW: {label} in {f.relative_to(d)}")
    hay = f"{name.replace('-', ' ')} {desc}".lower()
    tags = [c for c, rx in CATEGORIES.items() if re.search(rx, hay)] or ["general"]
    row = {"name": name, "tags": tags, "description": " ".join(desc.split())[:200],
           "source": source_of(d), "lines": n_lines, "has_scripts": (d / "scripts").is_dir()}
    return errors, warns, row


def library_names() -> list[str]:
    if not SKILLS.is_dir():
        return []
    return sorted(p.name for p in SKILLS.iterdir()
                  if p.is_dir() and not p.name.startswith(".") and (p / "SKILL.md").exists())


def rebuild_index(write_index: bool = True):
    """Validate all skills; write INDEX.md/json (+ VALIDATION.md). -> (rows, errors, warns)"""
    errors, warns, rows = [], [], []
    for d in sorted(p for p in SKILLS.iterdir() if p.is_dir() and not p.name.startswith(".")):
        e, w, row = check_skill(d)
        errors += [(d.name, m) for m in e]
        warns += [(d.name, m) for m in w]
        if row:
            rows.append(row)
    if write_index:
        atomic_write(LIB / "INDEX.json", json.dumps(rows, indent=1, ensure_ascii=False) + "\n")
        out = ["# Skill Library Index", "",
               f"{len(rows)} skills. Search: `skill-search <query>`. Add: `skill-add <name> [dir]`.", "",
               "| skill | tags | description | source | lines |", "|---|---|---|---|---|"]
        for r in rows:
            desc = r["description"].replace("|", "\\|")
            out.append(f"| {r['name']} | {','.join(r['tags'])} | {desc} | {r['source']} | {r['lines']} |")
        atomic_write(LIB / "INDEX.md", "\n".join(out) + "\n")
    rep = ["# Validation report", "", f"skills: {len(rows)}  errors: {len(errors)}  warnings: {len(warns)}", ""]
    rep += [f"- ERROR `{n}`: {m}" for n, m in errors] + [f"- WARN `{n}`: {m}" for n, m in warns]
    atomic_write(LIB / "VALIDATION.md", "\n".join(rep) + "\n")
    return rows, errors, warns


def load_index() -> list[dict]:
    rows = load_json(LIB / "INDEX.json", default=[])
    return rows if isinstance(rows, list) else []


# ───────────────────────────── packs ─────────────────────────────
def pack_path(name: str) -> Path:
    if not PACK_NAME.match(name or ""):
        raise SkillError(f"invalid pack name: {name!r} (kebab-case, optional one 'group/' prefix)")
    return PACKS / f"{name}.yaml"


def list_packs() -> list[str]:
    if not PACKS.is_dir():
        return []
    return sorted(str(p.relative_to(PACKS).with_suffix("")) for p in PACKS.rglob("*.yaml"))


def load_pack(name: str) -> dict:
    p = pack_path(name)
    if not p.exists():
        raise SkillError(f"pack not found: {name} (see: skill-add --packs)")
    data = yaml.safe_load(p.read_text()) or {}
    return {"name": name, "description": str(data.get("description") or ""),
            "extends": [str(x) for x in data.get("extends") or []],
            "skills": [str(x) for x in data.get("skills") or []]}


def save_pack(pack: dict) -> None:
    name = pack["name"]
    p = pack_path(name)
    skills = list(dict.fromkeys(pack.get("skills") or []))
    bad = [s for s in skills if not KEBAB.match(s)]
    if bad:
        raise SkillError(f"invalid skill names: {', '.join(bad)}")
    ext = list(dict.fromkeys(pack.get("extends") or []))
    if name in ext:
        raise SkillError("a pack cannot extend itself")
    for e in ext:
        pack_path(e)
    doc = {"name": name, "description": pack.get("description") or ""}
    if ext:
        doc["extends"] = ext
    doc["skills"] = skills
    with dir_lock(PACKS):
        before = p.read_text() if p.exists() else None
        atomic_write(p, yaml.safe_dump(doc, sort_keys=False, allow_unicode=True, width=100))
        try:  # validate cycles after save; restore the previous version on failure
            expand_pack(name)
        except SkillError as e:
            if "cycle" in str(e):
                if before is None:
                    p.unlink()
                else:
                    atomic_write(p, before)
                raise


def delete_pack(name: str) -> Path:
    p = pack_path(name)
    if not p.exists():
        raise SkillError(f"pack not found: {name}")
    users = [q for q in list_packs() if q != name and name in load_pack(q)["extends"]]
    if users:
        raise SkillError(f"pack {name} is extended by: {', '.join(users)}")
    dest = _trash_slot("pack", name.replace("/", "__"))
    shutil.move(str(p), dest / p.name)
    _write_trash_meta(dest, {"kind": "pack", "name": name, "origin": str(p), "removed_at": now()})
    return dest


def rename_pack(old: str, new: str) -> None:
    po, pn = pack_path(old), pack_path(new)
    if not po.exists():
        raise SkillError(f"pack not found: {old}")
    if pn.exists():
        raise SkillError(f"pack already exists: {new}")
    pk = load_pack(old)
    pk["name"] = new
    save_pack(pk)
    po.unlink()
    for q in list_packs():
        other = load_pack(q)
        if old in other["extends"]:
            other["extends"] = [new if e == old else e for e in other["extends"]]
            save_pack(other)


def expand_pack(name: str, _stack: tuple = ()) -> list[str]:
    if name in _stack:
        raise SkillError(f"pack cycle: {' -> '.join((*_stack, name))}")
    pk = load_pack(name)
    out: list[str] = []
    for e in pk["extends"]:
        out += expand_pack(e, (*_stack, name))
    out += pk["skills"]
    return list(dict.fromkeys(out))


def validate_packs() -> list[tuple[str, str]]:
    lib = set(library_names())
    probs = []
    for n in list_packs():
        try:
            pk = load_pack(n)
            expand_pack(n)
        except Exception as e:
            probs.append((n, str(e)))
            continue
        for s in pk["skills"]:
            if s not in lib:
                probs.append((n, f"skill not in library: {s}"))
    return probs


def packs_by_skill() -> dict[str, list[str]]:
    m: dict[str, list[str]] = {}
    for n in list_packs():
        with contextlib.suppress(Exception):
            for s in load_pack(n)["skills"]:
                m.setdefault(s, []).append(n)
    return m


def expand_items(items: list[str]) -> list[tuple[str, str | None]]:
    """['@web-vue', 'vitest'] -> [(skill, pack|None)...] (dedup, first pack wins)."""
    out: dict[str, str | None] = {}
    for it in items:
        if it.startswith("@"):
            for s in expand_pack(it[1:]):
                out.setdefault(s, it[1:])
        else:
            out.setdefault(it, None)
    return list(out.items())


# ───────────────────────────── trash + registry ─────────────────────────────
def _state_writable() -> bool:
    try:
        STATE.mkdir(parents=True, exist_ok=True)
        probe = STATE / ".probe"
        probe.write_text("")
        probe.unlink()
        return True
    except OSError:
        return False


def _trash_slot(kind: str, label: str, project: Path | None = None) -> Path:
    stamp = time.strftime("%Y%m%d-%H%M%S") + f"-{os.getpid()}-{time.monotonic_ns() % 10**6}"
    if _state_writable():
        base = STATE / "trash"
    elif project is not None:
        base = project / ".agents" / ".skill-trash"
        base.mkdir(parents=True, exist_ok=True)
        if not (base / ".gitignore").exists():
            (base / ".gitignore").write_text("*\n")
    else:
        base = CACHE / "trash"
    d = base / f"{stamp}-{kind}-{label}"
    d.mkdir(parents=True)
    return d


def _write_trash_meta(slot: Path, meta: dict) -> None:
    write_json(slot / "TRASH.json", meta)


def list_trash(project: Path | None = None) -> list[dict]:
    bases = [STATE / "trash", CACHE / "trash"]
    if project:
        bases.append(Path(project) / ".agents" / ".skill-trash")
    out = []
    for b in bases:
        if b.is_dir():
            for slot in sorted(b.iterdir(), reverse=True):
                m = load_json(slot / "TRASH.json")
                if m:
                    out.append({**m, "slot": str(slot), "id": slot.name})
    return out


def restore_trash(slot: str) -> str:
    slot_p = Path(slot)
    m = load_json(slot_p / "TRASH.json")
    if not m:
        raise SkillError(f"not a trash entry: {slot}")
    origin = Path(m["origin"])
    if origin.exists() or origin.is_symlink():
        raise SkillError(f"cannot restore, path exists: {origin}")
    payload = [p for p in slot_p.iterdir() if p.name != "TRASH.json"]
    if len(payload) != 1:
        raise SkillError("corrupt trash entry")
    origin.parent.mkdir(parents=True, exist_ok=True)
    shutil.move(str(payload[0]), origin)
    if m["kind"] == "skill":
        proj = Path(m["project"])
        _link(proj, m["name"])
        if m.get("manifest_entry"):
            with project_lock(proj):
                man = read_manifest(proj)
                man["skills"][m["name"]] = m["manifest_entry"]
                write_manifest(proj, man)
    shutil.rmtree(slot_p)
    return f"restored {m['kind']} {m['name']} → {origin}"


def empty_trash(slots: list[str] | None = None) -> int:
    entries = list_trash() if slots is None else [{"slot": s} for s in slots]
    n = 0
    for e in entries:
        p = Path(e["slot"])
        if (p / "TRASH.json").exists():
            shutil.rmtree(p)
            n += 1
    return n


def register_project(d: Path) -> None:
    try:
        with dir_lock(STATE):
            reg = load_json(STATE / "projects.json", default={"projects": []})
            ps = set(reg.get("projects", []))
            if str(d) not in ps:
                ps.add(str(d))
                write_json(STATE / "projects.json", {"projects": sorted(ps)})
    except OSError:
        pass  # sandboxed / read-only state dir: registry is best-effort


def registered_projects() -> list[Path]:
    reg = load_json(STATE / "projects.json", default={"projects": []})
    return [Path(p) for p in reg.get("projects", []) if (Path(p) / ".agents" / MANIFEST).exists()]


# ───────────────────────────── projects ─────────────────────────────
def resolve_project(d: str | Path = ".", git_root: bool = False) -> Path:
    p = Path(d).expanduser()
    if not p.is_dir():
        raise SkillError(f"target directory does not exist: {p}")
    p = p.resolve()
    if git_root:
        r = git(["rev-parse", "--show-toplevel"], cwd=p, timeout=10)
        if r.returncode == 0 and r.stdout.strip():
            p = Path(r.stdout.strip())
    return p


def check_name(name: str) -> None:
    if not KEBAB.match(name or ""):
        raise SkillError(f"invalid skill name: {name!r}")


def check_project_dirs(d: Path) -> None:
    """Refuse to write through symlinked .agents/.claude dirs (a repo could point them anywhere)."""
    d = Path(d)
    for sub in (".agents", ".agents/skills", ".claude", ".claude/skills"):
        if (d / sub).is_symlink():
            raise SkillError(f"{d / sub} is a symlink; refusing to write through it")


def project_lock(d: Path):
    check_project_dirs(d)
    return dir_lock(Path(d) / ".agents")


def read_manifest(d: Path) -> dict:
    m = load_json(Path(d) / ".agents" / MANIFEST)
    m.setdefault("version", 1)
    m.setdefault("skills", {})
    m.setdefault("packs", {})
    return m


def write_manifest(d: Path, m: dict) -> None:
    m["skills"] = dict(sorted(m["skills"].items()))
    write_json(Path(d) / ".agents" / MANIFEST, m)


def _dest(d: Path, name: str) -> Path:
    return Path(d) / ".agents" / "skills" / name


def _linkp(d: Path, name: str) -> Path:
    return Path(d) / ".claude" / "skills" / name


def _link(d: Path, name: str) -> None:
    lp = _linkp(d, name)
    lp.parent.mkdir(parents=True, exist_ok=True)
    target = f"../../.agents/skills/{name}"
    if lp.is_symlink() and os.readlink(lp) == target:
        return
    if lp.exists() and not lp.is_symlink():
        raise SkillError(f"{lp} exists and is not a symlink; refusing to overwrite")
    if lp.is_symlink():
        lp.unlink()
    os.symlink(target, lp)


def _link_ok(d: Path, name: str) -> bool:
    lp = _linkp(d, name)
    return lp.is_symlink() and (lp / "SKILL.md").is_file() and not (_dest(d, name) / "SKILL.md").is_symlink()


def _entry(name: str, src: Path, pack: str | None, prev: dict | None) -> dict:
    s = load_json(src / "SOURCE.json")
    e = dict(prev or {})
    e.update({"source": source_of(src), "upstream_commit": s.get("commit"),
              "tree_hash": tree_hash(src), "installed_at": now()})
    packs = list(e.get("packs") or [])
    if pack and pack not in packs:
        packs.append(pack)
    e["packs"] = packs
    e["manual"] = bool(e.get("manual")) or pack is None
    e.setdefault("pinned", False)
    return e


def install(d: Path, items: list[tuple[str, str | None]], force: bool = False) -> list[tuple[str, bool, str]]:
    """items: [(skill, pack|None)]. -> [(name, ok, message)]"""
    d = Path(d)
    res = []
    with project_lock(d):
        man = read_manifest(d)
        for name, pack in items:
            if not KEBAB.match(name):
                res.append((name, False, "invalid skill name")); continue
            src = SKILLS / name
            if not (src / "SKILL.md").is_file():
                res.append((name, False, f"not in library (try: skill-search {name.split('-')[0]})")); continue
            dest, lp = _dest(d, name), _linkp(d, name)
            if (lp.exists() or lp.is_symlink()) and not lp.is_symlink():
                res.append((name, False, f"{lp} exists and is not a symlink; refusing to overwrite")); continue
            if dest.is_symlink():
                res.append((name, False, f"{dest} is a symlink; refusing")); continue
            if dest.exists():
                if tree_hash(src) == tree_hash(dest):
                    state = "already up to date"
                elif force:
                    _to_trash_skill(d, name, man.get("skills", {}).get(name), reason="replaced by --force")
                    shutil.copytree(src, dest, symlinks=True)
                    state = "replaced (--force)"
                elif (man["skills"].get(name) or {}).get("tree_hash") == tree_hash(dest):
                    res.append((name, False, "outdated (library is newer) — run: skillcore.py update -C <dir> " + name)); continue
                else:
                    res.append((name, False, f"{dest} exists and differs from library "
                                             f"(review with: skillcore.py diff -C <dir> {name}; or use --force)")); continue
            else:
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copytree(src, dest, symlinks=True)
                state = "copied"
            _link(d, name)
            if not _link_ok(d, name):
                res.append((name, False, f"link does not resolve: {lp}")); continue
            man["skills"][name] = _entry(name, src, pack, man["skills"].get(name))
            if pack:
                man["packs"].setdefault(pack, {"installed_at": now()})
            res.append((name, True, state))
        write_manifest(d, man)
    register_project(d)
    return res


def _to_trash_skill(d: Path, name: str, entry: dict | None, reason: str = "uninstall") -> Path:
    dest = _dest(d, name)
    slot = _trash_slot("skill", name, project=d)
    shutil.move(str(dest), slot / name)
    _write_trash_meta(slot, {"kind": "skill", "name": name, "project": str(d), "origin": str(dest),
                             "removed_at": now(), "reason": reason, "manifest_entry": entry})
    return slot


def uninstall(d: Path, names: list[str]) -> list[tuple[str, bool, str]]:
    d = Path(d)
    res = []
    with project_lock(d):
        man = read_manifest(d)
        for name in names:
            if not KEBAB.match(name or ""):
                res.append((name, False, "invalid skill name")); continue
            dest, lp = _dest(d, name), _linkp(d, name)
            if not dest.exists() and not dest.is_symlink():
                had_link = lp.is_symlink()
                if had_link:
                    lp.unlink()
                if man["skills"].pop(name, None) is not None or had_link:
                    res.append((name, True, "removed stale manifest entry/link"))
                else:
                    res.append((name, False, "not installed"))
                continue
            if dest.is_symlink():
                res.append((name, False, f"{dest} is a symlink; refusing")); continue
            slot = _to_trash_skill(d, name, man["skills"].get(name))
            if lp.is_symlink():
                lp.unlink()
            man["skills"].pop(name, None)
            res.append((name, True, f"moved to trash: {slot}"))
        for p in list(man["packs"]):
            if not any(p in (e.get("packs") or []) for e in man["skills"].values()):
                man["packs"].pop(p)
        write_manifest(d, man)
    return res


def uninstall_pack(d: Path, pack: str) -> list[tuple[str, bool, str]]:
    """Drop a pack; delete only skills no other pack (or manual add) still needs."""
    d = Path(d)
    victims, res = [], []
    with project_lock(d):
        man = read_manifest(d)
        if pack not in man["packs"] and not any(pack in (e.get("packs") or []) for e in man["skills"].values()):
            return [(pack, False, "pack not installed in this project")]
        for name, e in man["skills"].items():
            ps = e.get("packs") or []
            if pack in ps:
                ps.remove(pack)
                if not ps and not e.get("manual"):
                    victims.append(name)
                else:
                    res.append((name, True, "kept (still used by " + (", ".join(ps) or "manual add") + ")"))
        man["packs"].pop(pack, None)
        write_manifest(d, man)
    return uninstall(d, victims) + res


def project_status(d: Path) -> list[dict]:
    """State per skill: ok | outdated | modified | conflict | missing | orphan | untracked | invalid."""
    d = Path(d)
    man = read_manifest(d)
    names = set(man["skills"])
    sk_dir = d / ".agents" / "skills"
    if sk_dir.is_dir():
        names |= {p.name for p in sk_dir.iterdir() if not p.name.startswith(".")}
    out = []
    for name in sorted(names):
        e = man["skills"].get(name)
        dest, src = _dest(d, name), SKILLS / name
        row = {"name": name, "packs": (e or {}).get("packs", []), "pinned": bool((e or {}).get("pinned")),
               "manual": bool((e or {}).get("manual")), "tracked": e is not None,
               "in_library": (src / "SKILL.md").is_file(), "link_ok": _link_ok(d, name) if dest.exists() else False}
        if dest.is_symlink() or (dest.exists() and not dest.is_dir()):
            row["state"] = "invalid"
        elif not dest.exists():
            row["state"] = "missing"
        elif e is None:
            row["state"] = "untracked"
            if row["in_library"]:
                row["same_as_library"] = tree_hash(dest) == tree_hash(src)
        else:
            cur = tree_hash(dest)
            lib = tree_hash(src) if row["in_library"] else None
            rec = e.get("tree_hash")
            if lib is None:
                row["state"] = "orphan" if cur == rec else "modified"
            elif cur == lib:
                row["state"] = "ok"
            elif cur != rec and lib != rec:
                row["state"] = "conflict"
            elif cur != rec:
                row["state"] = "modified"
            else:
                row["state"] = "outdated"
        out.append(row)
    return out


def dangling_links(d: Path) -> list[Path]:
    ld = Path(d) / ".claude" / "skills"
    if not ld.is_dir():
        return []
    return [p for p in ld.iterdir() if p.is_symlink() and not p.exists()
            and os.readlink(p).startswith("../../.agents/skills/")]


def repair_links(d: Path) -> list[str]:
    d = Path(d)
    msgs = []
    for p in dangling_links(d):
        p.unlink()
        msgs.append(f"removed dangling link {p.name}")
    sk = d / ".agents" / "skills"
    if sk.is_dir():
        for p in sk.iterdir():
            if p.is_dir() and not p.is_symlink() and not p.name.startswith(".") and not _link_ok(d, p.name):
                lp = _linkp(d, p.name)
                if lp.exists() and not lp.is_symlink():
                    msgs.append(f"skip {p.name}: {lp} is a real directory")
                    continue
                _link(d, p.name)
                msgs.append(f"linked {p.name}")
    return msgs


def update_project(d: Path, names: list[str] | None = None, force: bool = False) -> list[tuple[str, bool, str]]:
    """Refresh outdated skills from the library (pinned skipped; modified/conflict need force)."""
    d = Path(d)
    st = {r["name"]: r for r in project_status(d)}
    targets = names or [n for n, r in st.items() if r["state"] == "outdated"]
    for n in targets:
        check_name(n)
    res = []
    with project_lock(d):
        man = read_manifest(d)
        for name in targets:
            r = st.get(name)
            if not r:
                res.append((name, False, "not installed")); continue
            if r["state"] == "ok":
                res.append((name, True, "already up to date")); continue
            if r["pinned"] and not force:
                res.append((name, False, "pinned (unpin or --force)")); continue
            if r["state"] in ("modified", "conflict") and not force:
                res.append((name, False, f"{r['state']} locally (review diff; --force to replace)")); continue
            if r["state"] not in ("outdated", "modified", "conflict") or not r["in_library"]:
                res.append((name, False, f"cannot update from state {r['state']}")); continue
            src = SKILLS / name
            _to_trash_skill(d, name, man["skills"].get(name), reason="update")
            shutil.copytree(src, _dest(d, name), symlinks=True)
            _link(d, name)
            prev = man["skills"].get(name) or {}
            e = _entry(name, src, None, prev)
            e["manual"] = prev.get("manual", False)
            man["skills"][name] = e
            res.append((name, True, "updated"))
        write_manifest(d, man)
    return res


def adopt(d: Path, names: list[str] | None = None) -> list[tuple[str, bool, str]]:
    d = Path(d)
    st = {r["name"]: r for r in project_status(d)}
    targets = names or [n for n, r in st.items() if r["state"] == "untracked"]
    for n in targets:
        check_name(n)
    res = []
    with project_lock(d):
        man = read_manifest(d)
        for name in targets:
            r = st.get(name)
            if not r or r["state"] != "untracked":
                res.append((name, False, "not untracked")); continue
            if not r["in_library"]:
                res.append((name, False, "not in library (project-local skill; left as is)")); continue
            e = _entry(name, SKILLS / name, None, None)
            if not r.get("same_as_library"):
                e["tree_hash"] = tree_hash(_dest(d, name))  # treat current copy as baseline -> outdated
            try:
                _link(d, name)
            except SkillError as ex:
                res.append((name, False, str(ex))); continue
            man["skills"][name] = e
            res.append((name, True, "adopted" + ("" if r.get("same_as_library") else " (differs from library → outdated)")))
        write_manifest(d, man)
    register_project(d)
    return res


def set_pin(d: Path, names: list[str], pinned: bool) -> list[tuple[str, bool, str]]:
    for n in names:
        check_name(n)
    res = []
    with project_lock(d):
        man = read_manifest(d)
        for n in names:
            if n in man["skills"]:
                man["skills"][n]["pinned"] = pinned
                res.append((n, True, "pinned" if pinned else "unpinned"))
            else:
                res.append((n, False, "not tracked"))
        write_manifest(d, man)
    return res


def diff_text(a: Path, b: Path, label_a="library", label_b="project", max_lines=4000) -> str:
    a, b = Path(a), Path(b)
    files = set()
    for base in (a, b):
        if base.is_dir():
            files |= {str(p.relative_to(base)) for p in base.rglob("*") if p.is_file()
                      and not set(p.relative_to(base).parts) & HASH_IGNORE}
    out = []
    for rel in sorted(files):
        fa, fb = a / rel, b / rel
        try:
            la = fa.read_text(errors="replace").splitlines(keepends=True) if fa.exists() else []
            lb = fb.read_text(errors="replace").splitlines(keepends=True) if fb.exists() else []
        except OSError:
            continue
        if la != lb:
            out += difflib.unified_diff(la, lb, f"{label_a}/{rel}", f"{label_b}/{rel}")
        if len(out) > max_lines:
            out.append("\n… (diff truncated)\n")
            break
    return "".join(out) or "(no differences)\n"


def skill_tokens(d: Path) -> int:
    m = skill_meta(d)
    return (len(str(m.get("name", ""))) + len(" ".join(str(m.get("description", "")).split()))) // 4 + 8


def token_budget(d: Path) -> dict:
    proj = sorted((Path(d) / ".agents" / "skills").glob("*/SKILL.md"))
    p_tok = sum(skill_tokens(p.parent) for p in proj)
    seen, g_tok = set(), 0
    for gd in GLOBAL_SKILL_DIRS:
        if gd.is_dir():
            for p in gd.glob("*/SKILL.md"):
                rp = p.resolve()
                if rp not in seen:
                    seen.add(rp)
                    g_tok += skill_tokens(p.parent)
    return {"project_skills": len(proj), "project_tokens": p_tok, "global_skills": len(seen),
            "global_tokens": g_tok, "total": p_tok + g_tok, "budget": TOKEN_BUDGET}


def shadowing(d: Path) -> list[tuple[str, str]]:
    sk = Path(d) / ".agents" / "skills"
    if not sk.is_dir():
        return []
    out = []
    for p in sk.iterdir():
        for gd in GLOBAL_SKILL_DIRS:
            if (gd / p.name / "SKILL.md").exists() and (gd / p.name).resolve() != p.resolve():
                out.append((p.name, str(gd)))
    return out


def doctor(d: Path) -> list[tuple[str, str]]:
    """-> [(level, message)] level in {ok, warn, error}"""
    d = Path(d)
    out = []
    for r in project_status(d):
        s = r["state"]
        if s == "ok" and not r["link_ok"]:
            out.append(("warn", f"{r['name']}: .claude/skills link missing/broken (fix: repair-links)"))
        elif s in ("missing", "invalid"):
            out.append(("error", f"{r['name']}: {s} (manifest says installed)"))
        elif s in ("outdated", "modified", "conflict", "orphan", "untracked"):
            out.append(("warn", f"{r['name']}: {s}"))
    for p in dangling_links(d):
        out.append(("warn", f"dangling link .claude/skills/{p.name}"))
    for n, gd in shadowing(d):
        out.append(("warn", f"{n}: also installed globally in {gd} (name shadowing)"))
    tb = token_budget(d)
    lvl = "warn" if tb["total"] > tb["budget"] else "ok"
    out.append((lvl, f"skill descriptions ≈ {tb['total']} tokens ({tb['project_skills']} project ≈ {tb['project_tokens']}, "
                     f"{tb['global_skills']} global ≈ {tb['global_tokens']}) / budget {tb['budget']}"))
    man = read_manifest(d)
    for p in man["packs"]:
        try:
            missing = [s for s in expand_pack(p) if s not in man["skills"]]
            if missing:
                out.append(("warn", f"pack {p}: {len(missing)} skill(s) not installed: {', '.join(missing[:8])}"))
        except SkillError as e:
            out.append(("warn", f"pack {p}: {e}"))
    if not any(lv != "ok" for lv, _ in out):
        out.insert(0, ("ok", "no problems found"))
    return out


# ───────────────────────────── stack detection ─────────────────────────────
SUGGEST_RULES = [
    # (pack, file globs, content regex or None)
    ("web-vue", ["package.json"], r'"(vue|nuxt|@nuxt/[^"]+|pinia|vitepress)"\s*:'),
    ("web-react", ["package.json"], r'"(react|next|react-dom)"\s*:'),
    ("web-svelte", ["package.json"], r'"(svelte|@sveltejs/kit)"\s*:'),
    ("mobile-rn", ["package.json", "app.json"], r'"(expo|react-native)"'),
    ("node-backend", ["package.json"], r'"(@nestjs/core|hono|express|fastify|prisma|@prisma/client|@apollo/server|graphql)"\s*:'),
    ("cloudflare", ["wrangler.toml", "wrangler.json", "wrangler.jsonc"], None),
    ("python", ["pyproject.toml", "requirements*.txt", "setup.py", "uv.lock"], None),
    ("ml-train", ["pyproject.toml", "requirements*.txt"], r"\b(torch|transformers|trl|peft|unsloth|ultralytics|wandb|mlflow|datasets|accelerate)\b"),
    ("ml-serve", ["pyproject.toml", "requirements*.txt", "Dockerfile"], r"\b(vllm|bentoml|tritonserver|onnxruntime)\b"),
    ("llm-apps", ["pyproject.toml", "requirements*.txt", "package.json"], r"\b(langchain|langgraph|langsmith|llama[-_]index|llama_parse|anthropic|openai|qdrant|pymilvus|@anthropic-ai/sdk)\b"),
    ("data-eng", ["pyproject.toml", "requirements*.txt", "dbt_project.yml", "dagster.yaml"], r"\b(dbt|dagster|apache-airflow|airflow|polars|duckdb|pyspark|kafka|confluent|clickhouse)\b|^name:"),
    ("postgres", ["pyproject.toml", "requirements*.txt", "package.json", "docker-compose*.yml", "compose*.yml", "schema.prisma"], r"\b(psycopg|asyncpg|postgres|postgresql|pg|timescale|supabase|neon)\b"),
    ("nosql", ["pyproject.toml", "requirements*.txt", "package.json", "docker-compose*.yml", "compose*.yml"], r"\b(redis|ioredis|mongodb|mongoose|pymongo|neo4j)\b"),
    ("jvm", ["pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle.kts"], None),
    ("dotnet", ["*.csproj", "*.sln", "*.fsproj", "global.json"], None),
    ("flutter", ["pubspec.yaml"], None),
    ("docker", ["Dockerfile", "*.dockerfile", "docker-compose*.yml", "compose*.yml", "compose*.yaml"], None),
    ("iac", ["*.tf", "Pulumi.yaml", "*.pkr.hcl"], None),
    ("k8s-gitops", ["Chart.yaml", "kustomization.yaml", "kustomization.yml"], None),
    ("ci-github", [".github/workflows/*.yml", ".github/workflows/*.yaml"], None),
    ("web3", ["foundry.toml", "hardhat.config.*", "Scarb.toml", "Anchor.toml", "Move.toml"], None),
    ("qt", ["CMakeLists.txt", "*.pro", "*.qml"], r"(?i)\bqt[56]?\b|find_package\(Qt|import Qt"),
    ("research-writing", ["*.tex", "*.bib"], None),
]
SKIP_SCAN = {"node_modules", ".git", ".venv", "venv", "dist", "build", "target", ".next", ".nuxt", "__pycache__"}


def _scan_files(d: Path, depth: int = 3):
    d = Path(d)
    for root, dirs, files in os.walk(d):
        rel = Path(root).relative_to(d)
        dirs[:] = [x for x in dirs if x not in SKIP_SCAN and (len(rel.parts) < depth or x == "workflows")]
        for f in files:
            yield Path(root) / f


def suggest_packs(d: Path) -> list[tuple[str, str]]:
    """-> [(pack, reason)] for packs that exist in the library."""
    d = Path(d)
    files = list(_scan_files(d))
    available = set(list_packs())
    out = [("core", "baseline")] if "core" in available else []
    for pack, globs, rx in SUGGEST_RULES:
        if pack not in available:
            continue
        hits = [f for f in files if any(f.relative_to(d).match(g) or f.match(g) for g in globs)]
        for f in hits:
            if rx is None:
                out.append((pack, str(f.relative_to(d)))); break
            try:
                if f.stat().st_size < 2_000_000 and re.search(rx, f.read_text(errors="ignore"), re.M):
                    out.append((pack, str(f.relative_to(d)))); break
            except OSError:
                pass
    if any(p in ("web-vue", "web-react", "web-svelte") for p, _ in out) and "web-quality" in available:
        out.append(("web-quality", "web frontend detected"))
    return list({p: (p, r) for p, r in out}.values())


# ───────────────────────────── upstream: clones ─────────────────────────────
def remote_url(repo: str) -> str:
    return f"{GIT_BASE}{repo}.git"


def clone_dir(repo: str) -> Path:
    return REPO_CACHE / repo.replace("/", "__")


def ensure_clone(repo: str) -> Path:
    """Persistent blob-less clone (no checkout). Migrates a legacy $TMPDIR clone if present."""
    dest = clone_dir(repo)
    if (dest / ".git").exists() or (dest / "HEAD").exists():
        return dest
    REPO_CACHE.mkdir(parents=True, exist_ok=True)
    legacy = LEGACY_WORK / repo.replace("/", "__")
    if (legacy / ".git").exists():
        shutil.copytree(legacy, dest, symlinks=True)
        return dest
    r = git(["clone", "--depth", "1", "--filter=blob:none", "--sparse", "--no-checkout", "-q", remote_url(repo), str(dest)])
    if r.returncode != 0:
        shutil.rmtree(dest, ignore_errors=True)
        raise SkillError(f"clone failed for {repo}: {r.stderr.strip()[-200:]}")
    return dest


def have_commit(clone: Path, commit: str) -> bool:
    return git(["cat-file", "-e", f"{commit}^{{commit}}"], cwd=clone, timeout=30).returncode == 0


def fetch_commit(clone: Path, commit: str) -> None:
    if have_commit(clone, commit):
        return
    r = git(["fetch", "-q", "--depth", "1", "--filter=blob:none", "origin", commit], cwd=clone)
    if r.returncode != 0 or not have_commit(clone, commit):
        raise SkillError(f"cannot fetch {commit[:12]}: {r.stderr.strip()[-200:]}")


def remote_head(repo: str) -> str:
    r = git(["ls-remote", remote_url(repo), "HEAD"], timeout=60)
    if r.returncode != 0 or not r.stdout.strip():
        raise SkillError(f"ls-remote failed for {repo}: {(r.stderr.strip().splitlines() or ['no HEAD'])[-1][:200]}")
    return r.stdout.split()[0]


def tree_oid(clone: Path, commit: str, path: str) -> str | None:
    spec = f"{commit}:{'' if path in ('', '.') else path}"
    r = git(["rev-parse", spec], cwd=clone, timeout=30)
    return r.stdout.strip() if r.returncode == 0 else None


def root_licenses(clone: Path, commit: str) -> list[str]:
    r = git(["ls-tree", "--name-only", commit], cwd=clone, timeout=30)
    return [p for p in r.stdout.splitlines() if re.match(r"(?i)^(license|licence|copying)", p)]


_COMMENT_FM = re.compile(r"(\s*<!--.*?-->\s*)(---\n.*?\n---\n?)(.*)", re.S)


def _ignore(dirpath, names):
    out = {n for n in names if n in EXCLUDE_DIRS or n in EXCLUDE_FILES}
    if Path(dirpath).name == "agents":  # keep agents/openai.yaml, drop subagent definitions
        out |= {n for n in names if n.endswith(".md")}
    out |= {n for n in names if os.path.islink(os.path.join(dirpath, n))}  # never follow/copy symlinks
    return out


def _inline_repo_links(clone: Path, commit: str, wt: Path, sdir: Path, staged: Path, work: Path):
    """Upstream symlinks: copy the target's content if it stays inside the repo, else drop.
    Never follows a link to the local filesystem."""
    inlined, dropped = [], []
    links = sorted(p for p in sdir.rglob("*") if p.is_symlink())
    for link in links:
        rel = link.relative_to(sdir)
        if set(rel.parts) & EXCLUDE_DIRS or rel.name in EXCLUDE_FILES:
            continue
        tgt = os.readlink(link)
        repo_rel = os.path.normpath(os.path.join(os.path.dirname(str(link.relative_to(wt))), tgt))
        if os.path.isabs(tgt) or repo_rel == ".." or repo_rel.startswith("../"):
            dropped.append(str(rel)); continue
        git(["-c", "core.sparseCheckout=false", "--work-tree", str(wt), "checkout", commit, "--", repo_rel],
            cwd=clone, env={"GIT_INDEX_FILE": str(work / "index")}, timeout=300)
        src = wt / repo_rel
        if src.is_symlink() or not src.exists() or not src.resolve().is_relative_to(wt.resolve()):
            dropped.append(str(rel)); continue
        dest = staged / rel
        if not dest.parent.is_dir():
            continue
        if src.is_dir():
            shutil.copytree(src, dest, ignore=_ignore, symlinks=True)
        else:
            shutil.copy2(src, dest)
        inlined.append(str(rel))
    return inlined, dropped


def materialize(repo: str, commit: str, subpath: str, dest_name: str, tier: str = "", extra_source: dict | None = None):
    """Check out <repo>@<commit>:<subpath> into a staging dir ready to become skills/<dest_name>.
    -> (staged_dir, local_modifications[list]) ; raises SkillError."""
    clone = ensure_clone(repo)
    fetch_commit(clone, commit)
    work = Path(tempfile.mkdtemp(prefix="mat-", dir=CACHE))
    try:
        wt = work / "wt"
        wt.mkdir()
        lic_root = root_licenses(clone, commit)
        paths = (["."] if subpath in ("", ".") else [subpath]) + lic_root
        r = git(["-c", "core.sparseCheckout=false", "--work-tree", str(wt), "checkout", commit, "--", *paths],
                cwd=clone, env={"GIT_INDEX_FILE": str(work / "index")}, timeout=600)
        if r.returncode != 0:
            raise SkillError(f"checkout failed: {r.stderr.strip()[-200:]}")
        sdir = wt if subpath in ("", ".") else wt / subpath
        md = sdir / "SKILL.md"
        if md.is_symlink():
            raise SkillError("SKILL.md is a symlink (not imported by policy)")
        if not md.is_file():
            raise SkillError(f"{subpath}/SKILL.md missing at {commit[:12]}")
        lic_local = [p for p in sdir.iterdir() if re.match(r"(?i)^(licen[cs]e|copying)", p.name)]
        lic_root_p = [wt / p for p in lic_root if (wt / p).is_file()]
        if not lic_local and not lic_root_p:
            raise SkillError("no license found")
        staged = work / "out"
        shutil.copytree(sdir, staged, ignore=_ignore, symlinks=True)
        inlined, dropped = _inline_repo_links(clone, commit, wt, sdir, staged, work)
        if not lic_local:
            shutil.copy2(lic_root_p[0], staged / "UPSTREAM-LICENSE.txt")
        mods = []
        if inlined:
            mods.append(f"inlined {len(inlined)} in-repo symlink(s): {', '.join(inlined[:5])}")
        if dropped:
            mods.append(f"dropped {len(dropped)} symlink(s): {', '.join(dropped[:5])}")
        smd = staged / "SKILL.md"
        text = smd.read_text(encoding="utf-8", errors="replace")
        m = _COMMENT_FM.match(text)
        if not text.startswith("---") and m:
            comment, fm, body = m.groups()
            text = fm.rstrip("\n") + "\n\n" + comment.strip() + "\n\n" + body.lstrip("\n")
            mods.append("moved leading license comment below frontmatter so SKILL.md starts with ---")
        parts = text.split("---", 2)
        if text.startswith("---") and len(parts) == 3:
            fmd = yaml.safe_load(parts[1]) or {}
            if isinstance(fmd, dict) and fmd.get("name") != dest_name:
                mods.append(f"frontmatter name {fmd.get('name')!r} -> {dest_name!r}")
                fmd["name"] = dest_name
                text = "---\n" + yaml.safe_dump(fmd, sort_keys=False, allow_unicode=True) + "---" + parts[2]
        smd.write_text(text)
        src = {"repo": repo, "subpath": subpath, "commit": commit, "fetched_at": now(), "tier": tier,
               "license_review_required": True, **(extra_source or {})}
        if mods:
            src["local_modifications"] = mods
        write_json(staged / "SOURCE.json", src)
        final = Path(tempfile.mkdtemp(prefix=f"staged-{dest_name}-", dir=CACHE))
        final.rmdir()
        shutil.move(str(staged), final)
        return final, mods
    finally:
        shutil.rmtree(work, ignore_errors=True)


def install_into_library(staged: Path, name: str) -> bool:
    """Atomically swap staged dir into skills/<name>. -> existed_before"""
    target = SKILLS / name
    existed = target.exists()
    old = None
    if existed:
        old = CACHE / f"old-{name}-{os.getpid()}"
        shutil.rmtree(old, ignore_errors=True)
        os.rename(target, old)
    try:
        os.rename(staged, target)
    except OSError:
        shutil.move(str(staged), target)
    if old:
        shutil.rmtree(old, ignore_errors=True)
    return existed


# ───────────────────────────── upstream: updates ─────────────────────────────
_MECHANICAL = (re.compile(r"^frontmatter name "), re.compile(r"^moved leading license comment"),
               re.compile(r"^dropped \d+ symlink"), re.compile(r"^inlined \d+ in-repo symlink"))


def upstream_skills(names: list[str] | None = None) -> tuple[list[dict], list[str]]:
    """-> (skills with upstream provenance, names without upstream)"""
    pool = names or library_names()
    ups, none = [], []
    for n in pool:
        s = load_json(SKILLS / n / "SOURCE.json")
        if s.get("repo") and s.get("commit") and s.get("subpath") is not None:
            ups.append({"name": n, "repo": s["repo"], "subpath": s["subpath"], "commit": s["commit"],
                        "tier": s.get("tier", ""), "pinned": bool(s.get("pinned")),
                        "mods": s.get("local_modifications") or []})
        else:
            none.append(n)
    return ups, none


def _changed_files(old: Path, new: Path) -> tuple[list[str], list[str], list[str]]:
    def files(b):
        return {str(p.relative_to(b)): p for p in b.rglob("*") if p.is_file() and p.name != "SOURCE.json"}
    fo, fn = files(old), files(new)
    added = sorted(set(fn) - set(fo))
    removed = sorted(set(fo) - set(fn))
    changed = sorted(k for k in set(fo) & set(fn) if fo[k].read_bytes() != fn[k].read_bytes())
    return added, removed, changed


def assess(name: str, staged: Path, old_mods: list[str], new_mods: list[str]) -> list[str]:
    """Risk reasons for replacing skills/<name> with staged (empty = safe)."""
    cur = SKILLS / name
    reasons = []
    e_new, w_new, _ = check_skill(staged, name)
    _, w_old, _ = check_skill(cur, name) if cur.exists() else ([], [], None)
    for e in e_new:
        reasons.append(f"validation error: {e}")
    for w in sorted(set(w_new) - set(w_old)):
        if w.startswith("REVIEW:") or "allowed-tools" in w:
            reasons.append(f"new {w}")
    added, _, _ = _changed_files(cur, staged) if cur.exists() else ([], [], [])
    for a in added:
        p = staged / a
        if p.suffix in SCRIPT_EXT or os.access(p, os.X_OK):
            reasons.append(f"new executable/script: {a}")
    lost = [m for m in old_mods if not any(rx.match(m) for rx in _MECHANICAL) and m not in new_mods]
    for m in lost:
        reasons.append(f"manual local modification would be lost: {m}")
    return reasons


def check_updates(names: list[str] | None = None, jobs: int = 8, progress=None) -> dict:
    """Compare recorded commits with upstream HEAD (tree-OID per subpath, no blobs).
    -> {"items": [...], "no_upstream": [...], "errors": [...]}"""
    ups, none = upstream_skills(names)
    by_repo: dict[str, list[dict]] = {}
    for s in ups:
        by_repo.setdefault(s["repo"], []).append(s)
    items, errors = [], []

    def one(repo):
        group = by_repo[repo]
        try:
            head = remote_head(repo)
            res = []
            if all(s["commit"] == head for s in group):
                return [{**s, "status": "up-to-date", "new_commit": head} for s in group], None
            clone = ensure_clone(repo)
            fetch_commit(clone, head)
            for s in group:
                if s["commit"] == head:
                    res.append({**s, "status": "up-to-date", "new_commit": head}); continue
                try:
                    fetch_commit(clone, s["commit"])
                    old_oid = tree_oid(clone, s["commit"], s["subpath"])
                except SkillError:
                    old_oid = None
                new_oid = tree_oid(clone, head, s["subpath"])
                if new_oid is None:
                    st = "removed-upstream"
                elif old_oid == new_oid:
                    st = "up-to-date"
                elif not root_licenses(clone, head) and not any(
                        re.match(r"(?i)^(licen[cs]e|copying)", n) for n in git(
                            ["ls-tree", "--name-only", f"{head}:{'' if s['subpath'] in ('', '.') else s['subpath']}"],
                            cwd=clone).stdout.splitlines()):
                    st = "license-gone"
                else:
                    st = "changed"
                res.append({**s, "status": st, "new_commit": head, "old_tree": old_oid, "new_tree": new_oid})
            return res, None
        except (SkillError, subprocess.TimeoutExpired) as e:
            return [{**s, "status": "error", "error": str(e)} for s in group], f"{repo}: {e}"

    with ThreadPoolExecutor(max_workers=jobs) as ex:
        for repo, (res, err) in zip(by_repo, ex.map(one, by_repo), strict=True):
            items += res
            if err:
                errors.append(err)
            if progress:
                progress(repo, res)
    for it in items:
        if it["pinned"] and it["status"] == "changed":
            it["status"] = "pinned"
    items.sort(key=lambda x: (x["status"] != "changed", x["repo"], x["name"]))
    return {"items": items, "no_upstream": none, "errors": errors, "checked_at": now()}


def stage_update(item: dict) -> dict:
    """Materialize the new upstream version of one changed skill and assess risk."""
    staged, mods = materialize(item["repo"], item["new_commit"], item["subpath"], item["name"], item.get("tier", ""),
                               extra_source={"updated_from": item["commit"]})
    reasons = assess(item["name"], staged, item.get("mods", []), mods)
    a, r, c = _changed_files(SKILLS / item["name"], staged)
    return {**item, "staged": str(staged), "risk": reasons, "added": a, "removed": r, "changed_files": c}


def changelog(item: dict, limit: int = 30) -> str:
    clone = ensure_clone(item["repo"])
    r = git(["show", "-s", "--format=%ct", item["commit"]], cwd=clone, timeout=30)
    if r.returncode == 0 and r.stdout.strip():
        since = int(r.stdout.strip()) - 1
        git(["fetch", "-q", "--filter=blob:none", f"--shallow-since={since}", "origin", item["new_commit"]],
            cwd=clone, timeout=180)
    path = [] if item["subpath"] in ("", ".") else ["--", item["subpath"]]
    lg = git(["log", "--oneline", f"-n{limit}", f"{item['commit']}..{item['new_commit']}", *path], cwd=clone, timeout=120)
    return lg.stdout.strip() if lg.returncode == 0 and lg.stdout.strip() else "(changelog unavailable)"


def _lib_dirty(paths: list[str]) -> list[str]:
    r = git(["status", "--porcelain", "--", *paths], cwd=LIB, timeout=60)
    return [ln[3:] for ln in r.stdout.splitlines()] if r.returncode == 0 else []


def apply_updates(staged_items: list[dict], allow_risky: bool = False, commit: bool = True) -> list[tuple[str, bool, str]]:
    res, done = [], []
    for it in staged_items:
        name = it["name"]
        if it.get("risk") and not allow_risky:
            res.append((name, False, "risky: " + "; ".join(it["risk"]))); continue
        if not Path(it["staged"]).is_dir():
            res.append((name, False, "staged copy missing; re-run check")); continue
        dirty = _lib_dirty([f"skills/{name}"])
        if dirty:
            res.append((name, False, "uncommitted changes in library copy; commit or discard first")); continue
        install_into_library(Path(it["staged"]), name)
        done.append(it)
        res.append((name, True, f"updated {it['commit'][:8]} → {it['new_commit'][:8]}"))
    if done:
        rebuild_index()
        probs = validate_packs()
        _append_update_log(res, probs)
        if commit and (LIB / ".git").exists():
            git(["add", "--", *[f"skills/{i['name']}" for i in done], "INDEX.md", "INDEX.json", "VALIDATION.md",
                 "UPDATE_LOG.md"], cwd=LIB)
            body = "\n".join(f"- {i['name']}: {i['repo']} {i['commit'][:12]} -> {i['new_commit'][:12]}" for i in done)
            cr = git(["commit", "-q", "-m", f"Update {len(done)} skill(s) from upstream\n\n{body}"], cwd=LIB)
            if cr.returncode != 0:
                res.append(("git commit", False, (cr.stderr or cr.stdout).strip()[-200:] +
                            " — changes are applied but uncommitted"))
    return res


def _append_update_log(res, probs) -> None:
    p = LIB / "UPDATE_LOG.md"
    lines = [] if p.exists() else ["# Update log"]
    lines += ["", f"## {now()}", "", "| skill | result | detail |", "|---|---|---|"]
    lines += [f"| {n} | {'OK' if ok else 'SKIP'} | {m.replace('|', '/')} |" for n, ok, m in res]
    if probs:
        lines += ["", "Pack problems after update:"] + [f"- {a}: {b}" for a, b in probs]
    with open(p, "a") as f:
        f.write("\n".join(lines) + "\n")


def save_pending(items: list[dict]) -> None:
    write_json(UPDATES / "pending.json", {"saved_at": now(), "items": items})


def load_pending() -> list[dict]:
    items = load_json(UPDATES / "pending.json").get("items", [])
    return [i for i in items if Path(i.get("staged", "/nonexistent")).is_dir()]


def update_library(names: list[str] | None = None, mode: str = "check", allow_risky: bool = False,
                   commit: bool = True, progress=None) -> dict:
    """mode: check | apply | auto-safe."""
    rep = check_updates(names, progress=progress)
    changed = [i for i in rep["items"] if i["status"] == "changed"]
    staged, res = [], []
    if mode in ("apply", "auto-safe"):
        for it in changed:
            try:
                staged.append(stage_update(it))
            except SkillError as e:
                res.append((it["name"], False, f"stage failed: {e}"))
        res += apply_updates(staged, allow_risky=(allow_risky and mode == "apply"), commit=commit)
        risky = [s for s in staged if s.get("risk") and not (allow_risky and mode == "apply")]
        if risky:
            save_pending(risky)
        for s in staged:  # clean staged dirs that were applied (moved) or not risky
            if s not in risky:
                shutil.rmtree(s["staged"], ignore_errors=True)
    rep["staged"] = staged
    rep["results"] = res
    return rep


def set_library_pin(name: str, pinned: bool) -> None:
    p = SKILLS / name / "SOURCE.json"
    if not p.exists():
        raise SkillError(f"{name}: no SOURCE.json (no upstream to pin against)")
    s = load_json(p)
    if pinned:
        s["pinned"] = True
    else:
        s.pop("pinned", None)
    write_json(p, s)


def rollback(name: str, commit: bool = True) -> str:
    """Restore skills/<name> to the version before the last commit that touched it, and pin it."""
    r = git(["log", "-n", "1", "--format=%H", "--", f"skills/{name}"], cwd=LIB)
    last = r.stdout.strip()
    if not last:
        raise SkillError(f"{name}: no git history")
    if git(["cat-file", "-e", f"{last}^:skills/{name}/SKILL.md"], cwd=LIB).returncode != 0:
        raise SkillError(f"{name}: did not exist before {last[:8]} (nothing to roll back to)")
    if _lib_dirty([f"skills/{name}"]):
        raise SkillError(f"{name}: uncommitted changes; commit or discard first")
    git(["rm", "-rq", "--", f"skills/{name}"], cwd=LIB)
    co = git(["checkout", f"{last}^", "--", f"skills/{name}"], cwd=LIB)
    if co.returncode != 0:
        git(["checkout", "HEAD", "--", f"skills/{name}"], cwd=LIB)
        raise SkillError(f"rollback failed: {co.stderr.strip()}")
    if (SKILLS / name / "SOURCE.json").exists():
        set_library_pin(name, True)
    rebuild_index()
    if commit:
        git(["add", "-A", "--", f"skills/{name}", "INDEX.md", "INDEX.json", "VALIDATION.md"], cwd=LIB)
        cr = git(["commit", "-q", "-m", f"Roll back {name} to {last[:12]}^ (pinned)"], cwd=LIB)
        if cr.returncode != 0:
            return f"{name} rolled back and pinned, but git commit failed: {(cr.stderr or cr.stdout).strip()[-200:]}"
    return f"{name} rolled back to {last[:12]}^ and pinned (unpin: skillcore.py lib-unpin {name})"


def projects_affected(names: list[str] | None = None) -> list[tuple[Path, list[str]]]:
    out = []
    for p in registered_projects():
        st = [r["name"] for r in project_status(p) if r["state"] == "outdated" and (not names or r["name"] in names)]
        if st:
            out.append((p, st))
    return out


# ───────────────────────────── candidates (Upstream tab / tick script) ─────────────────────────────
_CAND = re.compile(r"^- \[([ xX])\] `([^`]+)`(.*)$")


def read_candidates() -> list[dict]:
    md = STAGING / "CANDIDATES.md"
    if not md.exists():
        return []
    out, repo = [], None
    for i, line in enumerate(md.read_text().splitlines()):
        m = re.match(r"^### (\S+) \(", line)
        if m:
            repo = m.group(1)
        m = _CAND.match(line)
        if m and repo:
            tail = m.group(3)
            sub = re.search(r"<sub>([^ ]+) ·([^<]*)</sub>", tail)
            out.append({"line": i, "repo": repo, "name": m.group(2), "ticked": m.group(1) != " ",
                        "path": sub.group(1) if sub else "", "flags": sub.group(2).strip() if sub else "",
                        "collides": bool(sub and "collides" in sub.group(2)),
                        "description": re.sub(r"\s*<sub>.*$", "", tail).lstrip(" —")})
    return out


def set_ticks(changes: dict[int, bool]) -> None:
    md = STAGING / "CANDIDATES.md"
    with dir_lock(STAGING):
        lines = md.read_text().splitlines()
        for i, on in changes.items():
            if _CAND.match(lines[i]):
                lines[i] = ("- [x]" if on else "- [ ]") + lines[i][5:]
        atomic_write(md, "\n".join(lines) + "\n")


# ───────────────────────────── CLI ─────────────────────────────
# ---------------------------------------------------------------- audit / verify / new

def _audit_targets(target: str | Path | None) -> list[Path]:
    """A skill dir, a project (its .agents/skills), or the whole library (target None)."""
    if target is None:
        if not SKILLS.is_dir():
            raise SkillError(f"no library skills dir at {SKILLS}")
        return sorted(p for p in SKILLS.iterdir() if p.is_dir() and not p.name.startswith("."))
    t = Path(target).expanduser()
    if (t / "SKILL.md").is_file() and not (t / "SKILL.md").is_symlink():
        return [t]
    sk = t / ".agents" / "skills"
    if sk.is_dir():  # symlinked skill dirs are kept so audit() can flag them
        return sorted(p for p in sk.iterdir() if (p.is_dir() or p.is_symlink()) and not p.name.startswith("."))
    raise SkillError(f"{t}: not a skill dir and no .agents/skills inside")


AUDIT_LEVELS = ["ok", "info", "review", "error"]


def _scan_text(d: Path, files: list[Path], skip: set[Path]) -> list[str]:
    """SUSPICIOUS patterns in every small text file not already covered by check_skill()."""
    out = []
    for f in files:
        if f in skip or f.is_symlink() or not f.is_file() or f.stat().st_size > 2_000_000:
            continue
        raw = f.read_bytes()
        if b"\0" in raw[:8192]:
            continue  # binary
        body = raw.decode("utf-8", errors="ignore")
        out += [f"{label} in {f.relative_to(d)}" for rx, label in SUSPICIOUS if re.search(rx, body, re.I)]
    return out


def audit(target: str | Path | None = None) -> list[dict]:
    """Static risk scan (offline): validation errors, risk patterns in every text file, symlinks, executables."""
    out = []
    for d in _audit_targets(target):
        if d.is_symlink():
            out.append({"name": d.name, "path": str(d), "level": "review", "errors": [],
                        "review": [f"skill dir is a symlink -> {os.readlink(d)} (not scanned)"], "executables": []})
            continue
        errors, warns, _ = check_skill(d)
        review = [w[len("REVIEW: "):] for w in warns if w.startswith("REVIEW:")]
        files = sorted(d.rglob("*"))
        covered = {d / "SKILL.md", *d.glob("scripts/*")}  # already scanned by check_skill
        review += _scan_text(d, files, covered)
        review += [f"symlink inside skill: {f.relative_to(d)}" for f in files if f.is_symlink()]
        exe = [str(f.relative_to(d)) for f in files
               if f.is_file() and not f.is_symlink() and (f.suffix in SCRIPT_EXT or os.access(f, os.X_OK))]
        level = "error" if errors else "review" if review else "info" if exe else "ok"
        out.append({"name": d.name, "path": str(d), "level": level, "errors": errors, "review": review, "executables": exe})
    return out


def audit_failed(rep: list[dict], fail_on: str) -> bool:
    if fail_on == "never":
        return False
    bar = AUDIT_LEVELS.index(fail_on)
    return any(AUDIT_LEVELS.index(r["level"]) >= bar for r in rep)


def verify_project(d: Path) -> list[tuple[str, bool, str]]:
    """Frozen check for CI: committed copies must match the manifest hashes. Needs no library."""
    d = Path(d)
    man = read_manifest(d)
    sk = d / ".agents" / "skills"
    on_disk = ({p.name for p in sk.iterdir() if (p.is_dir() or p.is_symlink()) and not p.name.startswith(".")}
               if sk.is_dir() else set())
    res = []
    for name in sorted(set(man["skills"]) | on_disk):
        e, dest = man["skills"].get(name), _dest(d, name)
        if e is None:
            res.append((name, False, "untracked: on disk but not in the manifest (run: skillcore.py adopt)"))
        elif dest.is_symlink() or not (dest / "SKILL.md").is_file():
            res.append((name, False, "missing, or not a real directory"))
        elif not _link_ok(d, name):
            res.append((name, False, "broken .claude/skills link (run: skillcore.py repair-links)"))
        elif tree_hash(dest) != e.get("tree_hash"):
            if e.get("pinned"):
                res.append((name, True, "pinned, edited locally"))
            else:
                res.append((name, False, "content differs from the manifest hash (edited? pin it, or run update)"))
        else:
            res.append((name, True, "ok"))
    return res


SKILL_TEMPLATE = """---
name: {name}
description: {description}
---

# {title}

## When to use
- The situations in which an agent should load this skill.

## Steps
1. Short, concrete instructions. Put long reference material in files next to this one and link them.

## Checks
- How the agent verifies the job is done.
"""


def new_skill(name: str, description: str = "") -> Path:
    """Scaffold a local skill in the library (no upstream, never touched by update-library)."""
    check_name(name)
    d = SKILLS / name
    if d.exists():
        raise SkillError(f"{name} already exists in the library")
    desc = " ".join(description.split()) or f"TODO: what {name} does and when an agent should use it."
    if len(desc) > 1024:
        raise SkillError("description is longer than 1024 characters")
    d.mkdir(parents=True)
    atomic_write(d / "SKILL.md", SKILL_TEMPLATE.format(name=name, description=json.dumps(desc, ensure_ascii=False),
                                                       title=name.replace("-", " ").capitalize()))
    atomic_write(d / "SOURCE.json", json.dumps({"origin": "local", "repo": None, "subpath": None, "commit": None,
                                                "created_at": now(), "tier": "local"}, indent=2) + "\n")
    rebuild_index()
    return d


def _print_results(res) -> int:
    rc = 0
    for name, ok, msg in res:
        print(f"{'✓' if ok else '✗'} {name}: {msg}", file=sys.stdout if ok else sys.stderr)
        rc |= 0 if ok else 1
    return rc


def _split_target(items: list[str], C: str | None) -> tuple[list[str], str]:
    """skill-add compat: trailing positional that is a path (not a library skill) = target dir."""
    if C is None and len(items) >= 2:
        last = items[-1]
        is_skill = bool(KEBAB.match(last)) and (SKILLS / last).is_dir()
        if not last.startswith("@") and not is_skill and (
                Path(last).is_dir() or "/" in last or last in (".", "..")):
            return items[:-1], last
    return items, C or "."


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(prog="skillcore.py", description="Cold skill library: packs, projects, upstream updates")
    sp = ap.add_subparsers(dest="cmd", required=True)

    def proj(p, nargs="*"):
        p.add_argument("-C", dest="C", help="project dir (default .)")
        return p

    p = proj(sp.add_parser("install", help="copy skills/@packs into a project (skill-add)"))
    p.add_argument("items", nargs="+"); p.add_argument("--force", action="store_true")
    p = proj(sp.add_parser("uninstall", help="remove skills or @pack from a project (to trash)"))
    p.add_argument("items", nargs="+")
    p = sp.add_parser("status", help="project skill status"); p.add_argument("dir", nargs="?", default=".")
    p.add_argument("--json", action="store_true")
    p = proj(sp.add_parser("update", help="refresh outdated project skills from the library"))
    p.add_argument("names", nargs="*"); p.add_argument("--force", action="store_true")
    p = sp.add_parser("update-projects", help="refresh outdated skills in all registered projects")
    p.add_argument("--dry-run", action="store_true")
    p = proj(sp.add_parser("adopt", help="track untracked skills already copied in a project")); p.add_argument("names", nargs="*")
    for c in ("pin", "unpin"):
        p = proj(sp.add_parser(c, help=f"{c} project skills (skip updates)")); p.add_argument("names", nargs="+")
    p = proj(sp.add_parser("diff", help="diff library vs project copy")); p.add_argument("name")
    p = sp.add_parser("repair-links", help="fix .claude/skills links"); p.add_argument("dir", nargs="?", default=".")
    p = sp.add_parser("doctor", help="check a project"); p.add_argument("dir", nargs="?", default=".")
    p = sp.add_parser("suggest", help="suggest packs for a project"); p.add_argument("dir", nargs="?", default=".")
    p = sp.add_parser("packs", help="list packs"); p.add_argument("-v", action="store_true")
    p = sp.add_parser("expand", help="print the skills of packs"); p.add_argument("packs", nargs="+")
    sp.add_parser("validate-packs", help="check packs reference existing skills; coverage")
    p = sp.add_parser("update-library", help="check/apply upstream updates")
    p.add_argument("names", nargs="*"); p.add_argument("--pack", action="append", default=[])
    g = p.add_mutually_exclusive_group()
    g.add_argument("--check", action="store_const", dest="mode", const="check")
    g.add_argument("--apply", action="store_const", dest="mode", const="apply")
    g.add_argument("--auto-safe", action="store_const", dest="mode", const="auto-safe")
    p.add_argument("--allow-risky", action="store_true"); p.add_argument("--no-commit", action="store_true")
    p.add_argument("--log", action="store_true", help="show upstream changelog for changed skills")
    p.add_argument("--json", action="store_true")
    p = sp.add_parser("rollback", help="restore previous library version of a skill (and pin it)"); p.add_argument("name")
    for c in ("lib-pin", "lib-unpin"):
        p = sp.add_parser(c, help=f"{c.split('-')[1]} a library skill against upstream updates"); p.add_argument("names", nargs="+")
    p = sp.add_parser("trash", help="list | restore ID | empty --yes")
    p.add_argument("action", choices=["list", "restore", "empty"]); p.add_argument("id", nargs="?")
    p.add_argument("--yes", action="store_true")
    sp.add_parser("rebuild-index", help="validate library and rebuild INDEX")
    p = sp.add_parser("audit", help="offline risk scan of a project's skills, one skill dir, or --library")
    p.add_argument("target", nargs="?"); p.add_argument("--library", action="store_true")
    p.add_argument("--fail-on", choices=["review", "error", "never"], default="review")
    p.add_argument("--json", action="store_true"); p.add_argument("-q", action="store_true", help="only list findings")
    p = sp.add_parser("verify", help="CI check: project skills match the manifest (no library needed)")
    p.add_argument("dir", nargs="?", default=".")
    p = sp.add_parser("new", help="scaffold a local skill in the library"); p.add_argument("name")
    p.add_argument("-d", "--description", default="")
    a = ap.parse_args(argv)

    try:
        if a.cmd == "install":
            items, target = _split_target(a.items, a.C)
            d = resolve_project(target)
            res = install(d, expand_items(items), force=a.force)
            for name, ok, msg in res:
                if ok:
                    print(f"✓ {name}: {msg} → {os.path.relpath(_dest(d, name))} (+ .claude/skills link)")
                else:
                    print(f"✗ {name}: {msg}", file=sys.stderr)
            return 0 if all(ok for _, ok, _ in res) else 1
        if a.cmd == "uninstall":
            d = resolve_project(a.C or ".")
            rc = 0
            for it in a.items:
                rc |= _print_results(uninstall_pack(d, it[1:]) if it.startswith("@") else uninstall(d, [it]))
            return rc
        if a.cmd == "status":
            d = resolve_project(a.dir)
            st = project_status(d)
            if a.json:
                print(json.dumps(st, indent=1)); return 0
            man = read_manifest(d)
            print(f"project: {d}\npacks: {', '.join(man['packs']) or '-'}")
            for r in st:
                flags = ("📌" if r["pinned"] else "") + ("" if r["link_ok"] or r["state"] in ("missing",) else " link!")
                print(f"  {r['state']:10} {r['name']:40} {','.join(r['packs']) or ('manual' if r['manual'] else '')} {flags}")
            return 0
        if a.cmd == "update":
            return _print_results(update_project(resolve_project(a.C or "."), a.names or None, force=a.force))
        if a.cmd == "update-projects":
            rc = 0
            for p_, names in projects_affected():
                print(f"# {p_}: {', '.join(names)}")
                if not a.dry_run:
                    rc |= _print_results(update_project(p_, names))
            return rc
        if a.cmd == "adopt":
            return _print_results(adopt(resolve_project(a.C or "."), a.names or None))
        if a.cmd in ("pin", "unpin"):
            return _print_results(set_pin(resolve_project(a.C or "."), a.names, a.cmd == "pin"))
        if a.cmd == "diff":
            check_name(a.name)
            d = resolve_project(a.C or ".")
            print(diff_text(SKILLS / a.name, _dest(d, a.name)), end=""); return 0
        if a.cmd == "repair-links":
            for m in repair_links(resolve_project(a.dir)) or ["nothing to repair"]:
                print(m)
            return 0
        if a.cmd == "doctor":
            res = doctor(resolve_project(a.dir))
            for lv, m in res:
                print(f"{ {'ok': '✓', 'warn': '!', 'error': '✗'}[lv]} {m}")
            return 1 if any(lv == "error" for lv, _ in res) else 0
        if a.cmd == "suggest":
            sg = suggest_packs(resolve_project(a.dir))
            for p_, why in sg:
                print(f"@{p_:20} ({why})")
            if sg:
                print("\nskill-add " + " ".join(f"@{p_}" for p_, _ in sg))
            return 0
        if a.cmd == "packs":
            for n in list_packs():
                try:
                    pk = load_pack(n)
                    cnt = len(expand_pack(n))
                    ext = f" (extends {', '.join(pk['extends'])})" if pk["extends"] else ""
                    print(f"@{n:28} {cnt:3} skills  {pk['description']}{ext}")
                    if a.v:
                        print("    " + " ".join(expand_pack(n)))
                except SkillError as e:
                    print(f"@{n:28} ERROR {e}")
            return 0
        if a.cmd == "expand":
            for n in a.packs:
                print("\n".join(expand_pack(n.lstrip("@"))))
            return 0
        if a.cmd == "validate-packs":
            probs = validate_packs()
            for n, m in probs:
                print(f"✗ {n}: {m}")
            covered = {s for n in list_packs() if not n.startswith("vendor/") for s in load_pack(n)["skills"]}
            lib = library_names()
            print(f"{len(list_packs())} packs · stack packs cover {len(covered & set(lib))}/{len(lib)} skills · problems: {len(probs)}")
            return 1 if probs else 0
        if a.cmd == "update-library":
            names = list(a.names)
            for pk in a.pack:
                names += expand_pack(pk.lstrip("@"))
            mode = a.mode or "check"
            rep = update_library(names or None, mode=mode, allow_risky=a.allow_risky, commit=not a.no_commit,
                                 progress=lambda repo, res: print(f"  checked {repo} ({len(res)})", file=sys.stderr))
            if a.json:
                print(json.dumps(rep, indent=1, default=str)); return 0
            cnt: dict[str, int] = {}
            for it in rep["items"]:
                cnt[it["status"]] = cnt.get(it["status"], 0) + 1
            print("summary: " + ", ".join(f"{k}={v}" for k, v in sorted(cnt.items())) + f", no-upstream={len(rep['no_upstream'])}")
            for it in rep["items"]:
                if it["status"] != "up-to-date":
                    print(f"  {it['status']:16} {it['name']:40} {it['repo']} {it['commit'][:8]}→{str(it.get('new_commit', ''))[:8]}")
                    if a.log and it["status"] == "changed":
                        print("      " + changelog(it).replace("\n", "\n      "))
            for s in rep["staged"]:
                if s.get("risk"):
                    print(f"  RISKY {s['name']}: " + "; ".join(s["risk"]))
            for e in rep["errors"]:
                print(f"  error: {e}", file=sys.stderr)
            rc = _print_results(rep["results"]) if rep["results"] else 0
            aff = projects_affected()
            if aff and mode != "check":
                print("projects with outdated skills (run: skillcore.py update-projects):")
                for p_, ns in aff:
                    print(f"  {p_}: {', '.join(ns)}")
            return rc
        if a.cmd == "rollback":
            print(rollback(a.name)); return 0
        if a.cmd in ("lib-pin", "lib-unpin"):
            for n in a.names:
                set_library_pin(n, a.cmd == "lib-pin")
                print(f"✓ {n}: {'pinned' if a.cmd == 'lib-pin' else 'unpinned'}")
            return 0
        if a.cmd == "trash":
            if a.action == "list":
                for e in list_trash():
                    print(f"{e['id']}  {e['kind']:5} {e['name']:35} {e.get('project', '')}")
                return 0
            if a.action == "restore":
                ent = [e for e in list_trash() if e["id"] == a.id]
                if not ent:
                    raise SkillError(f"no trash entry {a.id}")
                print(restore_trash(ent[0]["slot"])); return 0
            if not a.yes:
                raise SkillError("refusing to empty trash without --yes")
            print(f"deleted {empty_trash()} trash entries"); return 0
        if a.cmd == "audit":
            if a.library and a.target:
                raise SkillError("give either a target or --library, not both")
            rep = audit(None if a.library else resolve_project(a.target or "."))
            if a.json:
                print(json.dumps(rep, indent=1))
                return 1 if audit_failed(rep, a.fail_on) else 0
            icon = {"ok": "✓", "info": "·", "review": "!", "error": "✗"}
            cnt = {lv: sum(r["level"] == lv for r in rep) for lv in AUDIT_LEVELS}
            for r in rep:
                if a.q and r["level"] in ("ok", "info"):
                    continue
                print(f"{icon[r['level']]} {r['name']}" + (f"  ({len(r['executables'])} executable files)" if r["executables"] else ""))
                for m in r["errors"]:
                    print(f"    error: {m}")
                for m in r["review"]:
                    print(f"    review: {m}")
            print(f"{len(rep)} skills: " + ", ".join(f"{k}={v}" for k, v in cnt.items()))
            return 1 if audit_failed(rep, a.fail_on) else 0
        if a.cmd == "verify":
            d = resolve_project(a.dir)
            if not (d / ".agents" / "skill-manifest.json").is_file():
                raise SkillError(f"no .agents/skill-manifest.json in {d}")
            return _print_results(verify_project(d))
        if a.cmd == "new":
            d = new_skill(a.name, a.description)
            print(f"✓ created {d}/SKILL.md — edit it, then: skill-add {a.name}"); return 0
        if a.cmd == "rebuild-index":
            rows, e, w = rebuild_index()
            print(f"skills={len(rows)} errors={len(e)} warnings={len(w)}  (details: {LIB / 'VALIDATION.md'})")
            return 0
    except SkillError as e:
        print(f"skillcore: {e}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
