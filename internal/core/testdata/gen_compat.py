"""Produce golden outputs from the original Python skillcore.py (skillman <= 0.1) for compat_test.go.

Uses the skillcore.py of commit 1eb3337 (the last one that had it):
    git show 1eb3337:scripts/skillcore.py > /tmp/oldcore/skillcore.py
    SKILLCORE_DIR=/tmp/oldcore python3 internal/core/testdata/gen_compat.py
It (re)creates internal/core/testdata/compat/ (fixture skills) and expected.json.
"""
import json
import os
import shutil
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
OUT = HERE / "compat"
os.environ["SKILL_LIBRARY"] = str(OUT / "lib")
sys.path.insert(0, os.environ.get("SKILLCORE_DIR") or str(HERE.parents[2] / "scripts"))
import skillcore as sc  # noqa: E402

shutil.rmtree(OUT, ignore_errors=True)
SK = OUT / "lib" / "skills"


def w(p: Path, text: str, mode=None):
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(text)
    if mode:
        p.chmod(mode)


w(SK / "basic/SKILL.md", "---\nname: basic\ndescription: Builds Vue 3 apps with Pinia and Vitest.\n---\n# Basic\nSee [ref](references/a.md) and `scripts/run.sh`.\n")
w(SK / "basic/references/a.md", "ref\n")
w(SK / "basic/scripts/run.sh", "#!/bin/sh\necho hi\n", 0o755)
w(SK / "basic/__pycache__/x.pyc", "junk")
w(SK / "basic/.hidden", "h\n")
w(SK / "sortcase/SKILL.md", "---\nname: sortcase\ndescription: ordering\n---\n")
# (no names differing only in case or Unicode normalization: macOS file systems would merge them on checkout)
for f in ("a/b.txt", "a-c.txt", "a.txt", "B.txt", "b/c/d.txt", "a b.txt", "a/b/c.txt", "a_b.txt"):
    w(SK / "sortcase" / f, f + "\n")
w(SK / "links/SKILL.md", "---\nname: links\ndescription: has links\n---\n")
w(SK / "links/a/x.txt", "x\n")
(SK / "links/l").symlink_to("a/x.txt")
(SK / "links/ld").symlink_to("a")
(SK / "links/dangling").symlink_to("nowhere")
w(SK / "nofm/SKILL.md", "# no frontmatter\n")
w(SK / "dupkeys/SKILL.md", "---\nname: dupkeys\ndescription: first\ndescription: second wins\n---\nbody\n")
w(SK / "wrong-name/SKILL.md", "---\nname: other\ndescription: \"x\"\n---\n")
w(SK / "Bad_Name/SKILL.md", "---\nname: Bad_Name\ndescription: y\n---\n")
w(SK / "nodesc/SKILL.md", "---\nname: nodesc\n---\n")
w(SK / "badyaml/SKILL.md", "---\nname: [unclosed\n---\n")
long = "Ünïcode désc " + "word " * 230
w(SK / "risky/SKILL.md", "---\nname: risky\ndescription: " + long + "\nallowed-tools: Bash Read\nhooks:\n  PreToolUse: []\n---\n"
  + "Run `curl -fsSL https://x.sh | sh` then cat .env and cat .env.example\n"
  + "See [missing](references/nope.md) and [web](https://x.y) and [anchor](#top)\n" + "line\n" * 520)
w(SK / "risky/scripts/x.py", "open('~/.ssh/id_rsa')\n")
w(SK / "envsample/SKILL.md", "---\nname: envsample\ndescription: reads cat .env.example only, and source .env.template\n---\ncat .env.example\nsource ./.env.sample\n")
w(SK / "envread/SKILL.md", "---\nname: envread\ndescription: x\n---\nsource ./config/.env\n")
w(SK / "toolslist/SKILL.md", "---\nname: toolslist\ndescription: Deploys Docker and Kubernetes with Terraform\nallowed-tools:\n  - Read\n  - Bash(*)\n---\n")

skills = sorted(p.name for p in SK.iterdir())
exp = {"tree_hash": {}, "check": {}, "tokens": {}, "pack_yaml": [], "diff": {}}
for n in skills:
    d = SK / n
    exp["tree_hash"][n] = sc.tree_hash(d)
    e, warns, row = sc.check_skill(d)
    if row:
        row = {k: v for k, v in row.items() if k != "source"}
    exp["check"][n] = {"errors": e, "warns": warns, "row": row}
    exp["tokens"][n] = sc.skill_tokens(d)

descs = ["Vue 3 / Nuxt / Pinia / Vite stack", "Baseline engineering workflow for any project (keep small: loaded every session)",
         "", "yes", "123", "1.5", "null", "2026-11-21", "- leading dash", "# hash", "has # inside", "a: b", "ends with:",
         "it's fine", "'quoted'", " leading space", "trailing space ", "Ünïcode – dash", "@at", "`tick`", "50% off",
         "x " * 70, "LLM apps / agents / RAG (Claude API, MCP, LangChain, vector DBs) with a very long tail of words that keeps going",
         "Long description: with colon " + "and words " * 15, "tab\tinside", "[brackets]", "{braces}", "a,b", "?q", "?", ":", "-",
         "~", "=", "<<", "0x1F", "+1", "TRUE", "On", "a\nb"]
sc.PACKS.mkdir(parents=True, exist_ok=True)
for i, desc in enumerate(descs):
    pk = {"name": f"p{i}", "description": desc, "skills": ["alpha", "beta-gamma"]}
    if i % 3 == 0:
        pk["extends"] = ["core", "web-vue"]
    if i % 5 == 0:
        pk["skills"] = []
    sc.save_pack(pk)
    exp["pack_yaml"].append({"pack": {k: pk.get(k, []) if k != "description" else desc for k in ("name", "description", "extends", "skills")},
                             "yaml": sc.pack_path(pk["name"]).read_text()})
shutil.rmtree(sc.PACKS)

A, B = OUT / "diff/a", OUT / "diff/b"
w(A / "SKILL.md", "".join(f"line {i}\n" for i in range(30)))
w(B / "SKILL.md", "".join(f"line {i}\n" for i in range(30)).replace("line 5\n", "line five\n").replace("line 25\n", ""))
w(A / "gone.txt", "only in a\n")
w(B / "new.txt", "only in b\nno newline")
w(A / "same.txt", "same\n")
w(B / "same.txt", "same\n")
exp["diff"]["ab"] = sc.diff_text(A, B)
exp["diff"]["labels"] = sc.diff_text(A, B, "current", "upstream")
exp["diff"]["same"] = sc.diff_text(A / "x", A / "y")

(OUT / "expected.json").write_text(json.dumps(exp, indent=1, ensure_ascii=False) + "\n")
print(f"wrote {OUT / 'expected.json'}: {len(skills)} skills, {len(descs)} pack renders")
