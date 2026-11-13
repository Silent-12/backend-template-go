#!/bin/sh
# Cross-platform entry point. On Windows it delegates to hack/check.ps1 (same checks,
# same order); elsewhere it runs the identical steps with the tools installed by
# hack/install-tools.sh. Both scripts only report problems and never rewrite files.
set -eu
cd "$(dirname "$0")/.."

if [ "$(go env GOHOSTOS)" = windows ]; then
  # Windows PowerShell 5.1 ships with every supported Windows version; pwsh is optional.
  # Bypass applies to this process only, so a Restricted execution policy cannot disable the gate.
  if command -v powershell.exe >/dev/null 2>&1; then
    exec powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ./hack/check.ps1
  fi
  if command -v powershell >/dev/null 2>&1; then
    exec powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ./hack/check.ps1
  fi
  if command -v pwsh >/dev/null 2>&1; then
    exec pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -File ./hack/check.ps1
  fi
  echo 'No PowerShell found; install Windows PowerShell or pwsh, then retry.' >&2
  exit 1
fi

goimports='bin/tools/goimports'
revive='bin/tools/revive'
if [ ! -x "$goimports" ] || [ ! -x "$revive" ]; then
  echo 'Missing dev tools. Run sh ./hack/install-tools.sh first, then retry.' >&2
  exit 1
fi

module_name="$(go list -m)"
# Generated code is excluded from import-style and GoDoc checks but still compiled and tested.
source_files="main.go $(find api internal hack -name '*.go' ! -path 'internal/dao/*' ! -path 'internal/model/do/*' ! -path 'internal/model/entity/*')"
unformatted="$("$goimports" -l -local "$module_name" $source_files)"
if [ -n "$unformatted" ]; then
  printf 'Run goimports on:\n%s\n' "$unformatted" >&2
  exit 1
fi

# Revive v1.9 -exclude supports neither ** globs nor Windows backslash paths, so
# excluding tests through -exclude '**/*_test.go' silently did nothing. Build the
# exclusion list explicitly from the scanned files instead. The paths must go to
# -exclude rather than as positional arguments: a bare file argument is treated as
# its own single-file package, which turns package-comments into a false positive
# for every file that has no package doc of its own.
scanned_files="main.go $(find api internal hack -name '*.go' ! -path 'internal/dao/*' ! -path 'internal/model/do/*' ! -path 'internal/model/entity/*')"
test_files="$(find api internal hack -name '*_test.go' ! -path 'internal/dao/*' ! -path 'internal/model/do/*' ! -path 'internal/model/entity/*')"
test_count="$(printf '%s' "$test_files" | wc -w | tr -d ' ')"
lint_count="$(printf '%s' "$scanned_files" | wc -w | tr -d ' ')"
lint_count=$((lint_count - test_count))
if [ "$lint_count" -eq 0 ]; then
  echo 'No Go files left to lint; the file list is broken.' >&2
  exit 1
fi
echo "lint files: $lint_count (test files excluded: $test_count)"
set -- -config hack/revive.toml \
  -exclude './internal/dao/...' -exclude './internal/model/do/...' -exclude './internal/model/entity/...'
for test_file in $test_files; do
  set -- "$@" -exclude "$test_file"
done
set -- "$@" ./...
"$revive" "$@"

go run ./hack/quality
go vet ./...
go test ./...
go build -o bin/backend-template-go .
