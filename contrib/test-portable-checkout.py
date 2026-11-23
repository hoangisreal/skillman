"""Pass a real Git checkout between the native Windows and Unix CI jobs."""
import os
from pathlib import Path
import subprocess
import sys

phase = sys.argv[1]
work = Path(os.environ["RUNNER_TEMP"])
project = work / "Dự án portable"
bundle = work / "portable.bundle"
binary = work / ("skillman.exe" if os.name == "nt" else "skillman")
lib = work / "skill-library"
env = dict(os.environ, SKILL_LIBRARY=str(lib), SKILLMAN_STATE=str(work / "state"))
subprocess.run(["go", "build", "-o", binary, "./cmd/skillman"], check=True)


def git(*args):
    subprocess.run(["git", "-C", project, "-c", "user.name=CI", "-c",
                    "user.email=ci@example.invalid", *args], check=True)


def cli(*args):
    subprocess.run([binary, *args], env=env, cwd=project, check=True)


if phase == "create":
    project.mkdir()
    git("init", "-b", "main")
    (project / ".gitignore").write_text(".agents/.skillman.lock\n")
else:
    subprocess.run(["git", "-c", "core.autocrlf=true", "clone", bundle, project], check=True)
    cli("verify", "-C", project)

if phase != "verify":
    cli("init")
    skill = lib / "skills" / "portable-example"
    skill.mkdir()
    (skill / "SKILL.md").write_bytes(f"---\nname: portable-example\ndescription: Example {phase}\n---\n".encode())
    cli("add" if phase == "create" else "update", "-C", project, "portable-example")
    cli("verify", "-C", project)
    git("add", ".")
    git("commit", "-m", phase)
    git("bundle", "create", str(bundle), "--all")
