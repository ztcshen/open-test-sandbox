#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"

step() {
  printf '\n==> %s\n' "$*"
}

is_postgres_store_dsn() {
  [[ "$1" =~ ^[Pp][Oo][Ss][Tt][Gg][Rr][Ee][Ss]([Qq][Ll])?:// ]]
}

is_mysql_store_dsn() {
  [[ "$1" =~ ^[Mm][Yy][Ss][Qq][Ll]:// ]]
}

is_sqlite_store_dsn() {
  [[ "$1" =~ ^([Ss][Qq][Ll][Ii][Tt][Ee]://|[Ff][Ii][Ll][Ee]:) ]]
}

scope_paths=()
full_release_check=0
add_scope_path() {
  local path=$1
  path=${path#./}
  if [[ -n "$path" ]]; then
    scope_paths+=("$path")
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --full)
      full_release_check=1
      shift
      ;;
    --scope)
      if [[ $# -lt 2 ]]; then
        echo "--scope requires a path" >&2
        exit 1
      fi
      add_scope_path "$2"
      shift 2
      ;;
    --scope-file)
      if [[ $# -lt 2 ]]; then
        echo "--scope-file requires a file path" >&2
        exit 1
      fi
      if [[ ! -f "$2" ]]; then
        echo "--scope-file path does not exist: $2" >&2
        exit 1
      fi
      while IFS= read -r path; do
        add_scope_path "$path"
      done < "$2"
      shift 2
      ;;
    --)
      shift
      while [[ $# -gt 0 ]]; do
        add_scope_path "$1"
        shift
      done
      ;;
    -*)
      echo "unknown release-check option: $1" >&2
      exit 1
      ;;
    *)
      add_scope_path "$1"
      shift
      ;;
  esac
done

if [[ -n "${AGENT_TESTBENCH_RELEASE_CHECK_SCOPE:-}" ]]; then
  while IFS= read -r path; do
    add_scope_path "$path"
  done <<< "$AGENT_TESTBENCH_RELEASE_CHECK_SCOPE"
fi

if [[ ${#scope_paths[@]} -gt 0 ]]; then
  unique_scope_paths=()
  seen_path=""
  for path in "${scope_paths[@]}"; do
    duplicate=0
    if [[ ${#unique_scope_paths[*]} -gt 0 ]]; then
      for seen_path in "${unique_scope_paths[@]}"; do
        if [[ "$seen_path" == "$path" ]]; then
          duplicate=1
          break
        fi
      done
    fi
    if [[ "$duplicate" -eq 1 ]]; then
      continue
    fi
    unique_scope_paths+=("$path")
  done
  scope_paths=("${unique_scope_paths[@]}")
fi

scoped_release_check=0
if [[ ${#scope_paths[@]} -gt 0 ]]; then
  scoped_release_check=1
fi

if [[ "$scoped_release_check" -eq 1 && "$full_release_check" -eq 1 ]]; then
  echo "release-check cannot combine --full with --scope or --scope-file." >&2
  exit 1
fi

if [[ "$scoped_release_check" -eq 0 && "$full_release_check" -eq 0 ]]; then
  echo "release-check requires --scope, --scope-file, or --full." >&2
  echo "Slice validation: AGENT_TESTBENCH_SMOKE_STORE_DSN='sqlite:///tmp/agent-testbench-smoke.sqlite' npm run release-check -- --scope PATH" >&2
  echo "Scope file: AGENT_TESTBENCH_SMOKE_STORE_DSN='sqlite:///tmp/agent-testbench-smoke.sqlite' npm run release-check -- --scope-file .release-check-scope" >&2
  echo "Full sign-off: AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check -- --full" >&2
  exit 1
fi

if [[ "$scoped_release_check" -eq 1 ]]; then
  step "checking release scope"
  printf '  %s\n' "${scope_paths[@]}"
fi

step "checking whitespace"
if [[ "$scoped_release_check" -eq 1 ]]; then
  git diff --check -- "${scope_paths[@]}"
else
  git diff --check
fi

step "checking release gate tools"
for tool in rg sqlite3; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "$tool is required for release-check. Install it before running the release gate." >&2
    exit 1
  fi
done

step "checking SQL smoke Store"
if [[ -z "${AGENT_TESTBENCH_SMOKE_STORE_DSN:-${AGENT_TESTBENCH_SMOKE_STORE:-}}" ]]; then
  echo "AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE is required for release-check." >&2
  echo "SQL Store examples:" >&2
  echo "PostgreSQL: AGENT_TESTBENCH_SMOKE_STORE_DSN='postgres://user:pass@host:5432/agent_testbench_smoke?sslmode=disable' npm run release-check -- --full" >&2
  echo "MySQL: AGENT_TESTBENCH_SMOKE_STORE='mysql://user:pass@host:3306/agent_testbench_smoke?tls=false' npm run release-check -- --full" >&2
  echo "SQLite: AGENT_TESTBENCH_SMOKE_STORE='sqlite:///tmp/agent-testbench-smoke.sqlite' npm run release-check -- --scope PATH" >&2
  exit 1
fi
smoke_store_dsn="${AGENT_TESTBENCH_SMOKE_STORE_DSN:-${AGENT_TESTBENCH_SMOKE_STORE:-}}"
if is_postgres_store_dsn "$smoke_store_dsn"; then
  export AGENT_TESTBENCH_TEST_PG_DSN="${AGENT_TESTBENCH_TEST_PG_DSN:-$smoke_store_dsn}"
elif is_mysql_store_dsn "$smoke_store_dsn"; then
  mysql_dsn_info=$(node tools/smoke/mysql-store-dsn-guard.mjs "$smoke_store_dsn")
  mysql_parse_ok=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.parseOK))" "$mysql_dsn_info")
  mysql_scheme=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.scheme || '')" "$mysql_dsn_info")
  mysql_database=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(p.database || '')" "$mysql_dsn_info")
  mysql_safe_name=$(node -e "const p=JSON.parse(process.argv[1]); process.stdout.write(String(!!p.safeName))" "$mysql_dsn_info")
  if [[ "$mysql_parse_ok" != "true" || "$mysql_scheme" != "mysql" || -z "$mysql_database" ]]; then
    echo "AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE must be a mysql:// DSN with a database path." >&2
    exit 1
  fi
  if [[ "$mysql_safe_name" != "true" ]]; then
    echo "Refusing to run release-check against MySQL database '$mysql_database'." >&2
    echo "Use a dedicated sandbox/smoke/test/ci database name, not a business schema." >&2
    exit 1
  fi
  export AGENT_TESTBENCH_MYSQL_TEST_DSN="${AGENT_TESTBENCH_MYSQL_TEST_DSN:-$smoke_store_dsn}"
  export AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE="${AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE:-existing}"
elif is_sqlite_store_dsn "$smoke_store_dsn"; then
  if [[ "${AGENT_TESTBENCH_DISABLE_SQLITE_STORE:-}" == "1" ]]; then
    echo "AGENT_TESTBENCH_DISABLE_SQLITE_STORE cannot be combined with a SQLite release-check Store." >&2
    exit 1
  fi
else
  echo "AGENT_TESTBENCH_SMOKE_STORE_DSN or AGENT_TESTBENCH_SMOKE_STORE must be postgres://, postgresql://, mysql://, sqlite://, or file:." >&2
  exit 1
fi

step "checking SkyWalking smoke provider mode"
if [[ "${AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING:-}" == "1" ]]; then
  node tools/smoke/skywalking-release-guard.mjs "AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1"
  echo "Real SkyWalking validation required; using configured GraphQL endpoint and smoke trace ids." >&2
elif [[ -z "${AGENT_TESTBENCH_TRACE_GRAPHQL_URL:-}" ]]; then
  echo "AGENT_TESTBENCH_TRACE_GRAPHQL_URL is not set; smoke will use the deterministic synthetic SkyWalking GraphQL provider." >&2
  echo "Set AGENT_TESTBENCH_TRACE_GRAPHQL_URL, AGENT_TESTBENCH_SMOKE_TRACE_IDS, and AGENT_TESTBENCH_REQUIRE_REAL_SKYWALKING=1 for final live SkyWalking validation; synthetic smoke is not live topology proof." >&2
fi

step "checking generated state is not tracked"
if [[ -d team-configs ]]; then
  echo "root team-configs directory is not allowed in the core repository" >&2
  exit 1
fi

if [[ "$scoped_release_check" -eq 1 ]]; then
  tracked_generated_paths=()
  while IFS= read -r -d '' path; do
    case "$path" in
      .runtime/*|cmd/agent-testbench/.runtime/*|internal/server/controlplane/.runtime/*|node_modules/*|team-configs/*|test-private/*|test-results/*|coverage/*|*.db|*.sqlite|*.sqlite3)
        tracked_generated_paths+=("$path")
        ;;
    esac
  done < <(git ls-files -z -- "${scope_paths[@]}")
  if [[ ${#tracked_generated_paths[*]} -gt 0 ]]; then
    tracked_generated=$(printf '%s\n' "${tracked_generated_paths[@]}")
  else
    tracked_generated=""
  fi
else
  tracked_generated=$(git ls-files \
    '.runtime' \
    'cmd/agent-testbench/.runtime' \
    'internal/server/controlplane/.runtime' \
    'node_modules' \
    'team-configs' \
    'test-private' \
    'test-results' \
    'coverage' \
    '*.db' \
    '*.sqlite' \
    '*.sqlite3')
fi

if [[ -n "$tracked_generated" ]]; then
  echo "generated or local-only paths are tracked:" >&2
  echo "$tracked_generated" >&2
  exit 1
fi

step "checking source-domain guardrail"
if [[ "$scoped_release_check" -eq 1 ]]; then
  tools/guardrails/check_no_source_domain_core.sh "${scope_paths[@]}"
else
  tools/guardrails/check_no_source_domain_core.sh
fi

step "checking Store-first contract guardrail"
if [[ "$scoped_release_check" -eq 1 ]]; then
  tools/guardrails/check_store_first_contracts.sh "${scope_paths[@]}"
else
  tools/guardrails/check_store_first_contracts.sh
fi

if [[ "${AGENT_TESTBENCH_SKIP_QUALITY_GATE:-0}" == "1" ]]; then
  step "skipping Go AI quality gate"
else
  step "running Go AI quality gate"
  if [[ "$scoped_release_check" -eq 1 ]]; then
    QUALITY_GATE_STRICT="${QUALITY_GATE_STRICT:-false}" scripts/quality-gate.sh --report-dir build/reports/quality-gate "${scope_paths[@]}"
  else
    QUALITY_GATE_STRICT="${QUALITY_GATE_STRICT:-false}" scripts/quality-gate.sh --report-dir build/reports/quality-gate
  fi
fi

if [[ "$scoped_release_check" -eq 0 ]]; then
  step "running Go tests"
  if is_mysql_store_dsn "$smoke_store_dsn"; then
    go test -p 1 ./... -count=1
  else
    go test ./... -count=1
  fi

  step "running generic API case demo"
  if is_sqlite_store_dsn "$smoke_store_dsn"; then
    AGENT_TESTBENCH_CLEAN_DEMO_OUTPUT=1 AGENT_TESTBENCH_DEMO_STORE="$smoke_store_dsn" npm run demo:api-case
  else
    AGENT_TESTBENCH_CLEAN_DEMO_OUTPUT=1 AGENT_TESTBENCH_DISABLE_SQLITE_STORE=1 AGENT_TESTBENCH_DEMO_STORE="$smoke_store_dsn" npm run demo:api-case
  fi

  step "building React workbench"
  npm run build:frontend

  step "running frontend model tests"
  npm run test:frontend

  step "running smoke harness tests"
  node --test tools/examples/*.test.mjs tools/smoke/*.test.mjs

  step "running active SQL Store CLI smoke tests"
  if is_sqlite_store_dsn "$smoke_store_dsn"; then
    node tools/smoke/cli-active-store-smoke.mjs
  else
    npm run smoke:cli:sql-active
  fi

  if is_mysql_store_dsn "$smoke_store_dsn"; then
    step "running MySQL Store API smoke tests"
    AGENT_TESTBENCH_MYSQL_API_SMOKE_DSN="$smoke_store_dsn" npm run smoke:api:mysql-store
  fi

  step "running active SQL Store browser smoke tests"
  if is_sqlite_store_dsn "$smoke_store_dsn"; then
    node tools/smoke/control-plane-smoke.mjs
  else
    npm run smoke:frontend:sql-active
  fi
else
  node_scope_tests=()
  go_scope_all=0
  go_scope_packages=()
  run_frontend_tests=0
  run_frontend_build=0
  ran_scoped_runtime_tests=0

  add_go_scope_package() {
    local package=$1
    local seen_package
    if [[ ${#go_scope_packages[*]} -gt 0 ]]; then
      for seen_package in "${go_scope_packages[@]}"; do
        if [[ "$seen_package" == "$package" ]]; then
          return
        fi
      done
    fi
    go_scope_packages+=("$package")
  }

  go_test_packages=()
  add_go_test_package() {
    local package=$1
    local seen_package
    if [[ ${#go_test_packages[*]} -gt 0 ]]; then
      for seen_package in "${go_test_packages[@]}"; do
        if [[ "$seen_package" == "$package" ]]; then
          return
        fi
      done
    fi
    go_test_packages+=("$package")
  }

  collect_go_scope_package() {
    local path=$1
    local dir
    path=${path#./}

    case "$path" in
      go.mod|go.sum)
        go_scope_all=1
        return
        ;;
      *.go)
        dir=$(dirname -- "$path")
        if [[ "$dir" == "." ]]; then
          go_scope_all=1
        else
          add_go_scope_package "./$dir"
        fi
        return
        ;;
    esac

    case "$path" in
      cmd/*|internal/*)
        if [[ -d "$path" ]]; then
          if [[ -n "$(find "$path" -name '*.go' -print -quit)" ]]; then
            add_go_scope_package "./$path/..."
          fi
          return
        fi

        dir=$(dirname -- "$path")
        while [[ "$dir" != "." && "$dir" != "/" ]]; do
          if compgen -G "$dir/*.go" >/dev/null; then
            add_go_scope_package "./$dir"
            return
          fi
          case "$dir" in
            cmd|internal)
              return
              ;;
          esac
          dir=$(dirname -- "$dir")
        done
        ;;
    esac
  }

  for path in "${scope_paths[@]}"; do
    collect_go_scope_package "$path"

    case "$path" in
      control-plane/frontend/src/*|control-plane/frontend/build.mjs|package.json|package-lock.json)
        run_frontend_build=1
        run_frontend_tests=1
        ;;
      control-plane/static/demo-gallery.html|docs/demo-gallery.md|examples/demo-services/*|tools/examples/demo-service-server.mjs|tools/examples/demo-showcase.test.mjs)
        node_scope_tests+=("tools/examples/demo-showcase.test.mjs")
        ;;
      tools/examples/*.test.mjs|tools/smoke/*.test.mjs)
        node_scope_tests+=("$path")
        ;;
      .github/workflows/ci.yml)
        node_scope_tests+=("tools/smoke/ci-workflow.test.mjs")
        ;;
      tools/release-check.sh|tools/guardrails/*)
        node_scope_tests+=("tools/smoke/release-check.test.mjs")
        ;;
    esac
  done

  unique_node_scope_tests=()
  if [[ ${#node_scope_tests[*]} -gt 0 ]]; then
    for test_path in "${node_scope_tests[@]}"; do
      duplicate=0
      if [[ ${#unique_node_scope_tests[*]} -gt 0 ]]; then
        for seen_test_path in "${unique_node_scope_tests[@]}"; do
          if [[ "$seen_test_path" == "$test_path" ]]; then
            duplicate=1
            break
          fi
        done
      fi
      if [[ "$duplicate" -eq 0 ]]; then
        unique_node_scope_tests+=("$test_path")
      fi
    done
  fi
  if [[ ${#unique_node_scope_tests[*]} -gt 0 ]]; then
    node_scope_tests=("${unique_node_scope_tests[@]}")
  else
    node_scope_tests=()
  fi

  if [[ "$go_scope_all" -eq 1 || ${#go_scope_packages[*]} -gt 0 ]]; then
    step "running scoped Go tests"
    if [[ "$go_scope_all" -eq 1 ]]; then
      if is_mysql_store_dsn "$smoke_store_dsn"; then
        echo "  go test -p 1 ./... -count=1"
        go test -p 1 ./... -count=1
      else
        echo "  go test ./... -count=1"
        go test ./... -count=1
      fi
    else
      for package in "${go_scope_packages[@]}"; do
        add_go_test_package "$package"
      done

      target_go_imports=()
      while IFS= read -r import_path; do
        if [[ -n "$import_path" ]]; then
          target_go_imports+=("$import_path")
        fi
      done < <(go list "${go_scope_packages[@]}")

      if [[ ${#target_go_imports[*]} -gt 0 ]]; then
        while IFS= read -r package_line; do
          package=${package_line%% *}
          deps=""
          if [[ "$package_line" == *" "* ]]; then
            deps=${package_line#* }
          fi
          for target_import in "${target_go_imports[@]}"; do
            if [[ "$package" == "$target_import" ]]; then
              continue
            fi
            if [[ " $deps " == *" $target_import "* ]]; then
              add_go_test_package "$package"
              break
            fi
          done
        done < <(go list -f '{{.ImportPath}} {{join .Deps " "}}' ./...)
      fi

      if is_mysql_store_dsn "$smoke_store_dsn"; then
        printf '  go test -p 1 %s -count=1\n' "${go_test_packages[*]}"
        go test -p 1 "${go_test_packages[@]}" -count=1
      else
        printf '  go test %s -count=1\n' "${go_test_packages[*]}"
        go test "${go_test_packages[@]}" -count=1
      fi
    fi
    ran_scoped_runtime_tests=1
  fi

  if [[ "$run_frontend_build" -eq 1 ]]; then
    step "building React workbench"
    npm run build:frontend
    ran_scoped_runtime_tests=1
  fi

  if [[ "$run_frontend_tests" -eq 1 ]]; then
    step "running frontend model tests"
    npm run test:frontend
    ran_scoped_runtime_tests=1
  fi

  if [[ ${#node_scope_tests[@]} -gt 0 ]]; then
    step "running scoped Node tests"
    printf '  %s\n' "${node_scope_tests[@]}"
    node --test "${node_scope_tests[@]}"
    ran_scoped_runtime_tests=1
  fi

  if [[ "$ran_scoped_runtime_tests" -eq 0 ]]; then
    step "no scoped runtime tests selected"
  fi
fi

step "release check passed"
