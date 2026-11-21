#!/usr/bin/env bash
# install.sh — link skillman tools into ~/.local/bin and bootstrap a skill library.
#   ./install.sh                 # library at $SKILL_LIBRARY or ~/skill-library
#   SKILL_LIBRARY=/data/skills ./install.sh
# Idempotent. Never overwrites a real file: existing non-symlink tools are renamed *.bak-<timestamp>.
set -euo pipefail
REPO="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
LIB="${SKILL_LIBRARY:-$HOME/skill-library}"
BIN="${BIN_DIR:-$HOME/.local/bin}"
TS="$(date +%Y%m%d-%H%M%S)"

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1 ($2)" >&2; exit 1; }; }
need git "used for upstream clones and library history"
need python3 "3.11+"
python3 -c 'import sys; sys.exit(sys.version_info < (3, 11))' || { echo "python3 >= 3.11 required" >&2; exit 1; }
python3 -c 'import yaml' 2>/dev/null || { echo "PyYAML missing: pip install --user pyyaml (or dnf/apt install python3-yaml)" >&2; exit 1; }
command -v uv >/dev/null 2>&1 || echo "note: install uv (https://docs.astral.sh/uv/) to run the skillman TUI; the CLI works without it"

mkdir -p "$BIN"
for f in skillman skill-add skill-search; do
  dst="$BIN/$f"
  if [ -e "$dst" ] && [ ! -L "$dst" ]; then mv "$dst" "$dst.bak-$TS"; echo "backed up $dst -> $dst.bak-$TS"; fi
  ln -sfn "$REPO/bin/$f" "$dst"
done
ln -sfn "$REPO/bin/skillman.lock" "$BIN/skillman.lock"
echo "linked skillman, skill-add, skill-search -> $BIN"

mkdir -p "$LIB/skills" "$LIB/packs" "$LIB/staging"
[ -e "$LIB/scripts" ] || ln -s "$REPO/scripts" "$LIB/scripts"
for p in "$REPO"/packs/*.yaml; do [ -e "$LIB/packs/$(basename "$p")" ] || cp "$p" "$LIB/packs/"; done
[ -e "$LIB/staging/repos.txt" ] || cp "$REPO/staging/repos.txt" "$LIB/staging/repos.txt"
# the "skillman" skill teaches agents to use these tools; add it to a project with: skill-add skillman
# (refreshed on re-install unless you replaced it with your own skill of the same name)
SK="$LIB/skills/skillman"
if [ ! -e "$SK" ] || grep -q '"shipped_with": "skillman"' "$SK/SOURCE.json" 2>/dev/null; then
  if ! cmp -s "$REPO/skills/skillman/SKILL.md" "$SK/SKILL.md" 2>/dev/null; then
    mkdir -p "$SK" && cp "$REPO/skills/skillman/SKILL.md" "$SK/SKILL.md"
    printf '{\n  "origin": "local",\n  "repo": null,\n  "subpath": null,\n  "commit": null,\n  "tier": "local",\n  "shipped_with": "skillman"\n}\n' \
      > "$SK/SOURCE.json"
    python3 "$REPO/scripts/skillcore.py" rebuild-index >/dev/null && echo "installed skill 'skillman' into the library (skill-add skillman)"
  fi
fi
[ -e "$LIB/.gitignore" ] || printf '.cache/\n__pycache__/\n*.pyc\n.claude/\n.mcp.json\n' > "$LIB/.gitignore"
[ -d "$LIB/.git" ] || git -C "$LIB" init -q
echo "library ready at $LIB"

case ":$PATH:" in *":$BIN:"*) ;; *) echo "add $BIN to PATH";; esac
cat <<EOF

Next:
  python3 $LIB/scripts/verify-repos.py -j 8          # list candidate skills from staging/repos.txt
  python3 $LIB/scripts/tick-candidates.py --tier official --max-per-repo 40   # or tick in skillman tab 5
  python3 $LIB/scripts/import-skills.py && python3 $LIB/scripts/validate-library.py
  cd your-project && skill-add --suggest . && skillman
EOF
