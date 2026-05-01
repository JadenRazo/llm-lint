#!/usr/bin/env bash
# Seed a demo branch with intentional LLM-artifact violations so reviewers
# can screenshot the CI gate failing on a real PR.
#
# This script ONLY creates a local branch with one commit. It deliberately
# does NOT push or open a PR — review the diff first, then run the printed
# commands yourself.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "error: working tree is dirty. Stash or commit first." >&2
  exit 1
fi

current=$(git branch --show-current)
demo_branch="demo/llm-lint-violations"

if git show-ref --verify --quiet "refs/heads/$demo_branch"; then
  echo "error: branch $demo_branch already exists locally." >&2
  echo "       delete it first:  git branch -D $demo_branch" >&2
  exit 1
fi

git checkout -q -b "$demo_branch"

mkdir -p demo-fixtures
echo "project context for Claude" > demo-fixtures/CLAUDE.md
echo "editor rules"               > demo-fixtures/.cursorrules

git add demo-fixtures/
git commit -q \
  -m "demo: introduce intentional LLM artifacts (do not merge)" \
  -m "Ships CLAUDE.md and .cursorrules so the dogfood gate fails. Used for screenshot/launch material only — close without merging." \
  -m "Co-authored-by: Claude <noreply@anthropic.com>"

cat <<EOF

Demo branch ready (local only): $demo_branch

  Inspect:  git log --format=fuller -1
  Diff:     git show --stat HEAD

  Push and open a draft PR (when you're ready to capture screenshots):
    git push -u origin $demo_branch
    gh pr create --draft \\
      --title "DEMO: llm-lint violations (do not merge)" \\
      --body "Intentional violations for screenshot material. Demonstrates the dogfood gate failing on CLAUDE.md, .cursorrules, and a Claude trailer."

  Cleanup later:
    gh pr close --delete-branch <PR-number>
    git checkout $current
    git branch -D $demo_branch
EOF
