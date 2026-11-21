"""Throwaway library + fake upstream for tests (no network).

Importing this module points SKILL_LIBRARY / SKILLMAN_STATE / SKILLCORE_GIT_BASE at a fresh temp dir
*before* skillcore is imported, so tests can never touch the real library.
"""
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
T = Path(tempfile.mkdtemp(prefix="skillman-test-"))
LIBD, STATE, REMOTES = T / "lib", T / "state", T / "remotes"
os.environ.update(SKILL_LIBRARY=str(LIBD), SKILLMAN_STATE=str(STATE), SKILLCORE_GIT_BASE=f"file://{REMOTES}/",
                  GIT_AUTHOR_NAME="t", GIT_AUTHOR_EMAIL="t@t", GIT_COMMITTER_NAME="t", GIT_COMMITTER_EMAIL="t@t")
sys.path.insert(0, str(REPO / "scripts"))
import skillcore as sc  # noqa: E402

assert sc.LIB == LIBD, "skillcore must use the fixture library"
UP = T / "upwork"          # working clone of the fake upstream repo acme/skills
BARE = REMOTES / "acme" / "skills.git"
FAILS: list[str] = []


def check(cond, msg):
    print(("PASS " if cond else "FAIL ") + msg, flush=True)
    if not cond:
        FAILS.append(msg)


def sh(*a, cwd=None):
    r = subprocess.run(a, cwd=cwd, capture_output=True, text=True)
    assert r.returncode == 0, (a, r.stderr)
    return r.stdout.strip()


def mkskill(base: Path, name: str, desc="does things", body="body\n", extra=None):
    d = base / name
    d.mkdir(parents=True, exist_ok=True)
    (d / "SKILL.md").write_text(f"---\nname: {name}\ndescription: {desc}\n---\n{body}")
    for k, v in (extra or {}).items():
        (d / k).parent.mkdir(parents=True, exist_ok=True)
        (d / k).write_text(v)
    return d


def push_upstream(msg: str) -> str:
    sh("git", "add", "-A", cwd=UP)
    sh("git", "commit", "-qm", msg, cwd=UP)
    sh("git", "push", "-q", str(BARE), "main", cwd=UP)
    return sh("git", "rev-parse", "HEAD", cwd=UP)


def build(upstream_skills=("alpha", "beta", "gamma", "delta"), local_skills=("loc",)) -> str:
    """Create fake upstream acme/skills + a git-tracked library importing it. -> upstream commit."""
    UP.mkdir(parents=True)
    sh("git", "init", "-q", "-b", "main", cwd=UP)
    (UP / "LICENSE").write_text("MIT\n")
    for n in upstream_skills:
        mkskill(UP / "skills", n, desc=f"{n} skill for testing")
    sh("git", "add", "-A", cwd=UP)
    sh("git", "commit", "-qm", "v1", cwd=UP)
    c1 = sh("git", "rev-parse", "HEAD", cwd=UP)
    BARE.parent.mkdir(parents=True)
    sh("git", "clone", "-q", "--bare", str(UP), str(BARE))
    for k, v in (("uploadpack.allowFilter", "true"), ("uploadpack.allowAnySHA1InWant", "true")):
        sh("git", "config", k, v, cwd=BARE)
    (LIBD / "skills").mkdir(parents=True)
    sh("git", "init", "-q", "-b", "main", cwd=LIBD)
    (LIBD / ".gitignore").write_text(".cache/\n")
    for n in upstream_skills:
        staged, _ = sc.materialize("acme/skills", c1, f"skills/{n}", n, "official")
        sc.install_into_library(staged, n)
    for n in local_skills:
        mkskill(LIBD / "skills", n, desc=f"{n} local skill")
    sc.rebuild_index()
    sh("git", "add", "-A", cwd=LIBD)
    sh("git", "commit", "-qm", "init", cwd=LIBD)
    return c1


def finish():
    print(f"\n{'ALL PASS' if not FAILS else f'{len(FAILS)} FAILED: ' + '; '.join(FAILS)}  (tmp: {T})")
    if not FAILS:
        shutil.rmtree(T, ignore_errors=True)
    sys.exit(1 if FAILS else 0)
