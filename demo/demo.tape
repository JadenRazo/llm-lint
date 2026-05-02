# vhs tape for the llm-lint launch GIF.
# Run: bash demo/record.sh
# Or:  cd demo && vhs demo.tape
# Output: demo/demo.gif

Output demo.gif

Set Shell "bash"
Set FontSize 14
Set Width 1200
Set Height 720
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms
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
Type "clear"
Enter
Sleep 300ms
Show

# --- Scene 1: the artifacts land. ---
Type "# A feature branch lands with the usual LLM crumbs."
Sleep 1500ms
Enter

Type "echo 'project context for Claude' > CLAUDE.md"
Sleep 300ms
Enter

Type "echo 'editor rules' > .cursorrules"
Sleep 300ms
Enter

Type 'git add . && git commit -q -m "feat: ship the thing" -m "Co-authored-by: Claude <noreply@anthropic.com>"'
Sleep 1s
Enter
Sleep 1500ms

# --- Scene 2: scan and surface findings. One command, no install. ---
Type "# Catch them before they merge — no install needed:"
Sleep 1500ms
Enter

Type "npx @jadenrazo/llm-lint scan"
Sleep 300ms
Enter
Sleep 7s

# --- Scene 3: drill into a rule's full remediation. ---
Type "# Every finding ships with concrete remediation:"
Sleep 1500ms
Enter

Type "npx @jadenrazo/llm-lint rules show LLM003"
Sleep 300ms
Enter
Sleep 6s

# --- Tail. ---
Type "# In CI, --fail-on error blocks the merge."
Sleep 2500ms
Enter
Sleep 1500ms
