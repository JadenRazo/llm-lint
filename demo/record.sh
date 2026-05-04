#!/usr/bin/env bash
# Record the short autofix GIF. Wraps vhs with helpful preflight checks.
# Outputs: demo/demo.gif and demo/demo.mp4

set -euo pipefail

cd "$(dirname "$0")"

# `go install` drops binaries in $GOBIN or $GOPATH/bin (default ~/go/bin).
# Make sure those are visible; vhs is the most common offender here.
GOBIN="${GOBIN:-${GOPATH:-$HOME/go}/bin}"
case ":$PATH:" in
  *":$GOBIN:"*) ;;
  *) export PATH="$GOBIN:$PATH" ;;
esac

missing=0
for bin in vhs ttyd ffmpeg; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "missing: $bin" >&2
    missing=1
  fi
done

if [[ $missing -eq 1 ]]; then
  cat >&2 <<'EOF'

Install instructions:
  - vhs:    go install github.com/charmbracelet/vhs@latest
            (then ensure $GOPATH/bin or $HOME/go/bin is on PATH)
  - ttyd:   apt install ttyd          (Ubuntu/Debian)
            brew install ttyd         (macOS)
  - ffmpeg: apt install ffmpeg        (Ubuntu/Debian)
            brew install ffmpeg       (macOS)

Fallback without vhs/ttyd:
  python demo/render-autofix-demo.py
EOF
  exit 1
fi

echo "-> Recording demo.gif (this takes ~15s)..."
vhs demo.tape
echo
echo "ok Wrote $(pwd)/demo.gif ($(du -h demo.gif | cut -f1))"

ffmpeg -y -i demo.gif -movflags +faststart -pix_fmt yuv420p demo.mp4 >/dev/null 2>&1
echo "ok Wrote $(pwd)/demo.mp4 ($(du -h demo.mp4 | cut -f1))"
