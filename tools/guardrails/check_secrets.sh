#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=${AGENT_TESTBENCH_SECRET_SCAN_ROOT:-$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)}
cd "$ROOT_DIR"

fail=0
scan_args=("$@")
existing=()

collect_git_files() {
  local source=$1
  shift
  while IFS= read -r -d '' path; do
    case "$path" in
      .git/*|.idea/*|.runtime/*|build/*|coverage/*|node_modules/*|test-results/*)
        continue
        ;;
      .scratch/*|.understand-anything/*)
        if [[ "$source" == "untracked" ]]; then
          continue
        fi
        ;;
      tools/guardrails/check_secrets.sh)
        continue
        ;;
    esac
    if [[ -f "$path" ]]; then
      existing+=("$path")
    fi
  done < <("$@")
}

if [[ ${#scan_args[@]} -gt 0 ]]; then
  collect_git_files cached git ls-files --cached -z -- "${scan_args[@]}"
  collect_git_files untracked git ls-files --others --exclude-standard -z -- "${scan_args[@]}"
else
  collect_git_files cached git ls-files --cached -z
  collect_git_files untracked git ls-files --others --exclude-standard -z
fi

if [[ ${#existing[@]} -eq 0 ]]; then
  echo "no files to scan for secrets"
  exit 0
fi

key_files=()
for path in "${existing[@]}"; do
  case "$path" in
    *.pfx|*.PFX|*.p12|*.P12|*.jks|*.JKS|*.keystore|*.KEYSTORE|*.pem|*.PEM|*.key|*.KEY)
      key_files+=("$path")
      ;;
  esac
done

if [[ ${#key_files[@]} -gt 0 ]]; then
  echo "Secret-like certificate or key files are not allowed:" >&2
  printf '%s\n' "${key_files[@]}" >&2
  fail=1
fi

scan_pattern() {
  local pattern=$1
  local message=$2
  local matches
  matches=$(rg -n --with-filename --only-matching --replace '[REDACTED]' -e "$pattern" "${existing[@]}" || true)
  if [[ -n "$matches" ]]; then
    echo "$message" >&2
    echo "$matches" >&2
    fail=1
  fi
}

scan_pattern '(BEGIN (RSA |EC |OPENSSH |DSA )?PRIVATE KEY|AKIA[0-9A-Z]{16}|ghp_[A-Za-z0-9_]{36,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]+)' \
  "Secret-like token or private key content detected."

scan_pattern '(certificatePassword|certPassword|privateKeyPassword|accessKeySecret|secretKey)[[:space:]]*[:=][[:space:]]*[^${<[:space:]]' \
  "Plain secret-like configuration value detected."

if [[ "$fail" -eq 0 ]]; then
  echo "secret scan passed"
fi

exit "$fail"
