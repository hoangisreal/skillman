"""Render README screenshots (SVG) of every skillman tab against a COPY of a real library.

Usage: uv run --no-project --with textual==8.2.8 --with pyyaml python tools/make_screenshots.py [library-dir]
The library is rsync'ed to a temp dir first, so nothing in it is modified. Needs network for the
Updates tab (git ls-remote against upstream repos).
"""
import asyncio
import importlib.machinery
import importlib.util
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
SRC = Path(sys.argv[1] if len(sys.argv) > 1 else "~/skill-library").expanduser()
OUT = REPO / "docs" / "screenshots"
T = Path(tempfile.mkdtemp(prefix="shots-"))
LIB = T / "lib"
subprocess.run(["rsync", "-a", "--exclude=.claude", "--exclude=.mcp.json", "--exclude=.cache/updates",
                "--exclude=config.lock", f"{SRC}/", str(LIB)], check=True)
os.environ.update(SKILL_LIBRARY=str(LIB), SKILLMAN_STATE=str(T / "state"))
PROJ = T / "demo-app"
PROJ.mkdir()
(PROJ / "package.json").write_text('{"dependencies":{"vue":"^3","pinia":"^2","@nestjs/core":"^10","@prisma/client":"^5"}}')
(PROJ / "Dockerfile").write_text("FROM node:22\n")
(PROJ / ".github/workflows").mkdir(parents=True)
(PROJ / ".github/workflows/ci.yml").write_text("on: push\n")

loader = importlib.machinery.SourceFileLoader("skillman", str(REPO / "bin/skillman"))
spec = importlib.util.spec_from_loader("skillman", loader)
sm = importlib.util.module_from_spec(spec)
loader.exec_module(sm)
sc = sm.sc
sc.install(PROJ, sc.expand_items(["@core", "@web-vue", "postgres"]))
pv = PROJ / ".agents/skills/vitest/SKILL.md"
pv.write_text(pv.read_text() + "\nProject note: we use happy-dom.\n")  # show a "modified" row


async def main():
    OUT.mkdir(parents=True, exist_ok=True)
    app = sm.SkillMan(PROJ)
    async with app.run_test(size=(170, 46)) as pilot:
        async def shot(name):
            await pilot.pause(0.6)
            app.save_screenshot(f"{name}.svg", str(OUT))
            svg = OUT / f"{name}.svg"  # show a neutral path instead of the temp dir
            svg.write_text(svg.read_text().replace(str(T), "~/code").replace(str(T).replace(" ", "&#160;"), "~/code"))
            print("saved", name)

        lib = app.query_one(sm.LibraryTab)
        lib.query_one("#lib-search").value = "postgres"
        await pilot.pause()
        lib.table.focus()
        lib.table.move_cursor(row=1)
        await pilot.press("space", "space")
        await shot("1-library")
        await pilot.press("2")
        packs = app.query_one(sm.PacksTab)
        packs.table.move_cursor(row=packs.keys.index("fullstack-vue"))
        await shot("2-packs")
        await pilot.press("3")
        await shot("3-project")
        await pilot.press("4")
        up = app.query_one(sm.UpdatesTab)
        up.only_changes = False
        await pilot.press("c")
        for _ in range(600):
            await pilot.pause(0.1)
            if len(up.items) > 500:
                break
        up.reload()
        await shot("4-updates")
        changed = [k for k in up.keys if up.items[k]["status"] == "changed"]
        if changed:
            up.table.move_cursor(row=up.keys.index(changed[0]))
            await pilot.press("enter")
            for _ in range(300):
                await pilot.pause(0.1)
                if isinstance(app.screen, sm.TextView):
                    break
            await shot("7-update-review")
            await pilot.press("escape")
        await pilot.press("5")
        ut = app.query_one(sm.UpstreamTab)
        ut.query_one("#cand-search").value = "repo:trailofbits"
        await pilot.pause()
        ut.table.focus()
        await shot("5-upstream")
        await pilot.press("6")
        await shot("6-validation")
        await pilot.press("q")


asyncio.run(main())
shutil.rmtree(T, ignore_errors=True)
