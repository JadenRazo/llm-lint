# vhs tape for the llm-lint autofix GIF.
# Run: bash demo/record.sh
# Or:  cd demo && vhs demo.tape
# Output: demo/demo.gif

Output demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 30ms
Set CursorBlink false
Set Margin 20
Set MarginFill "#0a0a0a"
Set BorderRadius 8

# --- Silent setup: warm the npx cache, switch to a throwaway demo repo. ---
# We pre-resolve the package so the visible `npx ...` invocation runs in
# <1s instead of showing a download spinner mid-demo.
Hide
Type "npx -y @jadenrazo/llm-lint@latest version >/dev/null 2>&1"
Enter
Sleep 3s
Type "DEMO_DIR=$(mktemp -d -t llm-lint-demo.XXXX) && cd $DEMO_DIR"
Enter
Type "git init -q -b main && git config user.email demo@example.com && git config user.name 'Demo Dev'"
Enter
Type "echo 'project context for Claude' > CLAUDE.md"
Enter
Type "echo 'editor rules' > .cursorrules"
Enter
Type 'git add . && git commit -q -m "feat: ship the thing" -m "Co-authored-by: Claude <noreply@anthropic.com>"'
Enter
Type "clear"
Enter
Sleep 300ms
Show

# --- Scene 1: preview what autofix would do. ---
Type "npx @jadenrazo/llm-lint scan --fix-preview --fix-git-history latest --fail-on none --no-progress"
Sleep 300ms
Enter
Sleep 3500ms

# --- Scene 2: apply the same fixes. ---
Type "npx @jadenrazo/llm-lint scan --fix --fix-git-history latest --fail-on none --no-progress"
Sleep 300ms
Enter
Sleep 2500ms

# --- Scene 3: clean follow-up scan. ---
Type "npx @jadenrazo/llm-lint scan --fail-on none --no-progress"
Sleep 300ms
Enter
Sleep 2500ms
