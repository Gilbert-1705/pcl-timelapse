#!/usr/bin/env bash
set -uo pipefail

# Local, read-only security scan for macOS/Linux.
# Scanner output is not redirected to repository files.

ROOT="$(git rev-parse --show-toplevel 2>/dev/null)" || {
  echo "ERROR: Run this script from inside a Git repository."
  exit 2
}
cd "$ROOT"

failed=0
missing=0

run_check() {
  local label="$1"
  shift
  echo
  echo "===== ${label} ====="
  if ! "$@"; then
    echo "RESULT: FAIL – ${label}"
    failed=1
  else
    echo "RESULT: PASS – ${label}"
  fi
}

require_tool() {
  local tool="$1"
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "MISSING: ${tool}"
    missing=1
  fi
}

echo "Repository: $ROOT"
echo "Branch: $(git branch --show-current 2>/dev/null || echo detached)"
echo "Commit: $(git rev-parse HEAD 2>/dev/null || echo unknown)"

echo
echo "===== Git working tree ====="
git status --short
git diff --check || failed=1

require_tool gitleaks
require_tool trivy
require_tool semgrep

if command -v gitleaks >/dev/null 2>&1; then
  # Scans the current repository and reachable history. Redaction is enabled.
  run_check "Gitleaks history and repository scan" \
    gitleaks git --redact=100 --log-opts=--all .
fi

if command -v trivy >/dev/null 2>&1; then
  run_check "Trivy filesystem scan" \
    trivy fs --scanners vuln,misconfig,secret \
      --severity HIGH,CRITICAL --exit-code 1 .
fi

if command -v semgrep >/dev/null 2>&1; then
  run_check "Semgrep static analysis" \
    semgrep scan --config auto --error .
fi

echo
echo "===== Summary ====="
if [ "$missing" -ne 0 ]; then
  echo "BLOCKED: One or more required tools are missing. Install them and rerun."
  exit 2
fi

if [ "$failed" -ne 0 ]; then
  echo "BLOCKED: One or more checks failed. Review findings and rerun."
  exit 1
fi

echo "SCANNERS PASSED. This is not a guarantee of security or publication readiness."
echo "Complete the manual review checklist, inspect files and Git history, and review CI results."
exit 0
