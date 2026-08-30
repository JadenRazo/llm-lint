#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="${LLM_LINT_POLICY_ROOT:-$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)}"
workflow_dir="${repo_root}/.github/workflows"
failures=()

while IFS= read -r finding; do
  value="${finding#*uses:}"
  value="${value%%#*}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"

  if [[ "${value}" == ./* ]] ||
     [[ "${value}" =~ ^docker://[^[:space:]@]+@sha256:[0-9a-f]{64}$ ]] ||
     [[ "${value}" =~ ^[^[:space:]@]+@[0-9a-f]{40}$ ]]; then
    continue
  fi
  failures+=("${finding}")
done < <(grep -RInE --include='*.yml' --include='*.yaml' \
  '^[[:space:]]*(-[[:space:]]+)?uses:' "${workflow_dir}" || true)

while IFS= read -r finding; do
  [[ -z "${finding}" ]] || failures+=("${finding}")
done < <(grep -n '^FROM ' "${repo_root}/Dockerfile" |
  grep -Ev '^.*:FROM [^[:space:]@]+@sha256:[0-9a-f]{64}([[:space:]]+AS[[:space:]]+[^[:space:]]+)?$' || true)

while IFS= read -r finding; do
  [[ -z "${finding}" ]] || failures+=("${finding}")
done < <(grep -RInE --include='*.yml' --include='*.yaml' \
  'go install [^[:space:]]+@(latest|main|master|HEAD)([[:space:]]|$)' \
  "${workflow_dir}" || true)

if ((${#failures[@]})); then
  echo "Workflow/container dependencies must use immutable commits, digests, or versions:" >&2
  printf '%s\n' "${failures[@]}" >&2
  exit 1
fi

echo "Workflow Actions, container bases, and tool installs are immutable."
