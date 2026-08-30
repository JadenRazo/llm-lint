#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
fixture="$(mktemp -d /tmp/llm-lint-workflow-policy.XXXXXX)"
cleanup() {
  case "${fixture}" in
    /tmp/llm-lint-workflow-policy.*) /usr/bin/rm -rf --one-file-system -- "${fixture}" ;;
    *) echo "Refusing unsafe fixture cleanup: ${fixture}" >&2 ;;
  esac
}
trap cleanup EXIT

mkdir -p "${fixture}/.github/workflows"

write_safe_fixture() {
  printf '%s\n' \
    'name: fixture' \
    'jobs:' \
    '  check:' \
    '    steps:' \
    '      - uses: actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803 # v6' \
    '      - uses: docker://example.invalid/tool@sha256:1111111111111111111111111111111111111111111111111111111111111111' \
    '      - run: go install example.invalid/tool@v1.2.3' \
    >"${fixture}/.github/workflows/fixture.yml"
  printf '%s\n' \
    'FROM example.invalid/base@sha256:2222222222222222222222222222222222222222222222222222222222222222' \
    >"${fixture}/Dockerfile"
}

expect_rejection() {
  if LLM_LINT_POLICY_ROOT="${fixture}" "${script_dir}/check-workflow-pins.sh" >/dev/null 2>&1; then
    echo "Expected immutable workflow policy rejection: $1" >&2
    exit 1
  fi
}

write_safe_fixture
LLM_LINT_POLICY_ROOT="${fixture}" "${script_dir}/check-workflow-pins.sh" >/dev/null

sed -i 's/actions\/checkout@[0-9a-f]*/actions\/checkout@v6/' \
  "${fixture}/.github/workflows/fixture.yml"
expect_rejection "mutable Action"

write_safe_fixture
sed -i 's/@sha256:[0-9a-f]*/:latest/' "${fixture}/Dockerfile"
expect_rejection "mutable container base"

write_safe_fixture
sed -i 's/tool@v1.2.3/tool@latest/' "${fixture}/.github/workflows/fixture.yml"
expect_rejection "mutable Go install"

echo "Immutable workflow policy negative cases pass."
