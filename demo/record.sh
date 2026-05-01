#!/usr/bin/env bash
# Record the launch GIF. Wraps vhs with helpful preflight checks.
# Output: demo/demo.gif

set -euo pipefail

cd "$(dirname "$0")"

# `go install` drops binaries in $GOBIN or $GOPATH/bin (default ~/go/bin).
# Make sure those are visible — vhs is the most common offender here.
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
EOF
  exit 1
fi

echo "→ Recording demo.gif (this takes ~30s)..."
vhs demo.tape
echo
echo "✓ Wrote $(pwd)/demo.gif ($(du -h demo.gif | cut -f1))"
