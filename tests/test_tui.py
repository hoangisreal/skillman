"""skillman TUI smoke test with Textual's pilot on a fixture library (no network).
Run: uv run --no-project --with textual==8.2.8 --with pyyaml python tests/test_tui.py
"""
import asyncio
import importlib.machinery
import importlib.util
import subprocess

from fixtures import LIBD, REPO, T, UP, build, check, finish, mkskill, push_upstream, sc

build(upstream_skills=("alpha", "beta", "gamma"), local_skills=("vue", "vitest", "tdd", "review-pr"))
sc.save_pack({"name": "core", "description": "baseline", "skills": ["tdd", "review-pr"]})
sc.save_pack({"name": "web-vue", "description": "vue stack", "skills": ["vue", "vitest"]})
sc.save_pack({"name": "fullstack", "description": "web + acme", "extends": ["web-vue"], "skills": ["alpha"]})
(LIBD / "staging").mkdir()
(LIBD / "staging/CANDIDATES.md").write_text("# c\n\n### acme/skills (official) · `abc` · LICENSE\n"
                                             "- [ ] `alpha` — d <sub>skills/alpha · 5L · collides</sub>\n"
                                             "- [ ] `newone` — d <sub>skills/newone · 5L</sub>\n")
subprocess.run(["git", "-C", str(LIBD), "add", "-A"], check=True)
subprocess.run(["git", "-C", str(LIBD), "commit", "-qm", "packs"], check=True)
PROJ = T / "demo-app"
PROJ.mkdir()
(PROJ / "package.json").write_text('{"dependencies":{"vue":"^3"}}')

loader = importlib.machinery.SourceFileLoader("skillman", str(REPO / "bin/skillman"))
spec = importlib.util.spec_from_loader("skillman", loader)
sm = importlib.util.module_from_spec(spec)
loader.exec_module(sm)
assert sm.sc is sc


async def wait_for(pilot, cond, tries=120):
    for _ in range(tries):
        await pilot.pause(0.1)
        if cond():
            return True
    return False


async def main():
    app = sm.SkillMan(PROJ)
    async with app.run_test(size=(160, 45)) as pilot:
        lib = app.query_one(sm.LibraryTab)
        check(len(lib.keys) == len(app.index) == 7, f"library rows {len(lib.keys)}")
        lib.query_one("#lib-search").value = "pack:core"
        await pilot.pause()
        check(sorted(lib.keys) == ["review-pr", "tdd"], f"search pack:core -> {lib.keys}")
        lib.query_one("#lib-search").value = "src:acme"
        await pilot.pause()
        check(len(lib.keys) == 3, f"search src:acme -> {lib.keys}")
        lib.table.focus()
        await pilot.press("space", "space", "i")
        await pilot.pause()
        await pilot.press("n")  # cancel keeps the selection
        await pilot.pause()
        check(len(lib.selected) == 2 and not sc.read_manifest(PROJ)["skills"], "cancel install keeps selection")
        await pilot.press("i")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause()
        check(sorted(sc.read_manifest(PROJ)["skills"]) == ["alpha", "beta"], "install library selection")

        await pilot.press("2")
        await pilot.pause()
        packs = app.query_one(sm.PacksTab)
        packs.table.focus()
        packs.table.move_cursor(row=packs.keys.index("fullstack"))
        await pilot.pause()
        await pilot.press("i")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause()
        man = sc.read_manifest(PROJ)
        check({"vue", "vitest"} <= set(man["skills"]) and "fullstack" in man["packs"], "install pack with extends")
        await pilot.press("n")
        await pilot.pause()
        await pilot.press(*"my-pack", "enter")
        await pilot.pause()
        check(sc.pack_path("my-pack").exists(), "create pack")
        packs.table.move_cursor(row=packs.keys.index("my-pack"))
        await pilot.pause()
        await pilot.press("a")
        await pilot.pause()
        await pilot.press(*"gamma", "enter")
        await pilot.pause()
        check(sc.load_pack("my-pack")["skills"] == ["gamma"], "add skill to pack via picker")

        await pilot.press("3")
        await pilot.pause()
        proj = app.query_one(sm.ProjectTab)
        check(sorted(proj.keys) == ["alpha", "beta", "vitest", "vue"], f"project rows {proj.keys}")
        proj.table.focus()
        proj.table.move_cursor(row=proj.keys.index("vue"))
        await pilot.press("x")
        await pilot.pause()
        await pilot.press("y")
        await pilot.pause()
        check("vue" not in sc.read_manifest(PROJ)["skills"], "remove skill (to trash)")
        await pilot.press("o")
        await pilot.pause()
        check(isinstance(app.screen, sm.TextView), "doctor view")
        await pilot.press("escape")
        await pilot.press("s")
        await pilot.pause()
        check(isinstance(app.screen, sm.Pick), "suggest picker")
        await pilot.press("escape")

        await pilot.press("7")
        await pilot.pause()
        tr = app.query_one(sm.TrashTab)
        tr.table.focus()
        slot = next(s for s, e in tr.entries.items() if e["name"] == "vue")
        tr.table.move_cursor(row=tr.keys.index(slot))
        await pilot.press("R")
        await pilot.pause()
        check("vue" in sc.read_manifest(PROJ)["skills"], "restore from trash")

        # upstream changes: alpha safe, beta risky (adds hooks)
        (UP / "skills/alpha/SKILL.md").write_text("---\nname: alpha\ndescription: alpha v2\n---\nv2\n")
        (UP / "skills/beta/SKILL.md").write_text("---\nname: beta\ndescription: b\nhooks:\n  Stop: []\n---\nb2\n")
        push_upstream("v2")
        await pilot.press("4")
        await pilot.pause()
        up = app.query_one(sm.UpdatesTab)
        up.table.focus()
        await pilot.press("c")
        check(await wait_for(pilot, lambda: "alpha" in up.keys and "beta" in up.keys), f"check all: {up.keys}")
        await pilot.press("space", "space", "a")
        check(await wait_for(pilot, lambda: isinstance(app.screen, sm.Pick)), "risky choice dialog")
        await pilot.press("escape")
        await pilot.pause()
        check("v2" not in (LIBD / "skills/alpha/SKILL.md").read_text(), "esc on risky dialog cancels everything")
        await pilot.press("a")
        check(await wait_for(pilot, lambda: isinstance(app.screen, sm.Pick)), "risky choice dialog again")
        await pilot.press("enter")  # first option: safe only
        check(await wait_for(pilot, lambda: isinstance(app.screen, sm.TextView)), "apply result")
        await pilot.press("escape")
        check("v2" in (LIBD / "skills/alpha/SKILL.md").read_text(), "safe update applied")
        check("hooks" not in (LIBD / "skills/beta/SKILL.md").read_text(), "risky update not applied")
        log = subprocess.run(["git", "-C", str(LIBD), "log", "-1", "--format=%s"], capture_output=True, text=True).stdout
        check("Update 1 skill" in log, f"library commit: {log.strip()}")
        await pilot.press("3")
        await pilot.pause()
        check(dict((r["name"], r["state"]) for r in app.status).get("alpha") == "outdated", "project shows outdated")
        proj.table.focus()
        await pilot.press("U")
        await pilot.pause()
        check(dict((r["name"], r["state"]) for r in app.status).get("alpha") == "ok", "update all outdated")

        await pilot.press("5")
        await pilot.pause()
        ut = app.query_one(sm.UpstreamTab)
        ut.table.focus()
        ut.table.move_cursor(row=ut.keys.index(next(k for k, c in ut.cands.items() if c["name"] == "newone")))
        await pilot.press("space")
        await pilot.pause()
        check(any(c["ticked"] and c["name"] == "newone" for c in sc.read_candidates()), "tick candidate")

        await pilot.press("6")
        await pilot.pause()
        vt = app.query_one(sm.ValidationTab)
        mkskill(LIBD / "skills", "broken", body="see [x](references/missing.md)\n")
        vt.table.focus()
        await pilot.press("v")
        await pilot.pause()
        check("broken" in vt.names.values(), "re-validate finds new warning")
        vt.table.focus()
        await pilot.press("enter")
        await pilot.pause()
        await pilot.pause()
        check(app.query_one("TabbedContent").active == "t-lib", "jump to library")
        await pilot.press("q")


asyncio.run(main())
finish()
