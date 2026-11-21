#!/usr/bin/env bash
# install.sh — build skillman, link it into ~/.local/bin and bootstrap a skill library.
#   ./install.sh                      # library at $SKILL_LIBRARY or ~/skill-library
#   ./install.sh --no-global-skill    # do not link the skillman skill into ~/.claude/skills and ~/.agents/skills
#   SKILL_LIBRARY=/data/skills BIN_DIR=~/bin ./install.sh
# Idempotent. Never overwrites a real file: existing non-symlink tools are renamed *.bak-<timestamp>.
set -euo pipefail
REPO="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"
BIN="${BIN_DIR:-$HOME/.local/bin}"
TS="$(date +%Y%m%d-%H%M%S)"
GLOBAL_SKILL=1
for a in "$@"; do
  case "$a" in
    --no-global-skill) GLOBAL_SKILL=0 ;;
    -h|--help) sed -n '2,7p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $a" >&2; exit 2 ;;
  esac
done

need() { command -v "$1" >/dev/null 2>&1 || { echo "missing: $1 ($2)" >&2; exit 1; }; }
need git "used for upstream clones and library history"
need go "Go 1.26+ builds the single skillman binary: https://go.dev/dl/"

mkdir -p "$BIN"
backup() { if [ -e "$1" ] && [ ! -L "$1" ]; then mv "$1" "$1.bak-$TS"; echo "backed up $1 -> $1.bak-$TS"; fi; }
ours() { [ -x "$1" ] && "$1" --version 2>/dev/null | grep -q '^skillman '; }
ours "$BIN/skillman" || backup "$BIN/skillman"   # a previous build of ours is simply replaced
(cd "$REPO" && go build -trimpath -ldflags "-s -w" -o "$BIN/skillman.new" ./cmd/skillman)
mv -f "$BIN/skillman.new" "$BIN/skillman"
for f in skill-add skill-search; do
  backup "$BIN/$f"
  ln -sfn skillman "$BIN/$f"
done
rm -f "$BIN/skillman.lock" 2>/dev/null || true   # left over from the Python TUI (<= 0.1)
echo "installed $BIN/skillman (+ skill-add, skill-search) — $("$BIN/skillman" --version)"

"$BIN/skillman" init
if [ "$GLOBAL_SKILL" = 1 ]; then
  # lets Claude Code / Codex / OpenCode use skillman in every project (one short skill description)
  "$BIN/skillman" agent-setup || echo "note: agent-setup skipped some targets (see above)"
fi

case ":$PATH:" in *":$BIN:"*) ;; *) echo "add $BIN to PATH";; esac
LIB="${SKILL_LIBRARY:-$HOME/skill-library}"
cat <<EOF

Next:
  skillman upstream verify -j 8                       # list candidate skills from $LIB/staging/repos.txt
  skillman upstream tick --tier official --max-per-repo 40   # or tick in the TUI (tab 6, space)
  skillman upstream import                            # import through the safety gates
  cd your-project && skillman recommend . && skillman  # pick skills, or browse them in the TUI (tab 2)
EOF
