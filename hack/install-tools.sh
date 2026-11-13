#!/bin/sh
# Install the pinned dev tools into bin/tools and enable this repository's Git hooks.
# hack/install-tools.ps1 installs the same versions on Windows; keep both lists identical.
set -eu
cd "$(dirname "$0")/.."

if [ ! -f .githooks/pre-commit ]; then
  echo 'Missing .githooks/pre-commit; cannot enable the pre-commit hook.' >&2
  exit 1
fi
if ! repository_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  echo 'Run this script from a cloned Git repository.' >&2
  exit 1
fi
# Only ever change hooks of this repository, never the enclosing one.
if [ "$(cd "$repository_root" && pwd -P)" != "$(pwd -P)" ]; then
  echo 'The project must be its own Git repository; refusing to change the parent repository hooks.' >&2
  exit 1
fi

GOBIN="$(pwd)/bin/tools"
export GOBIN
go install golang.org/x/tools/cmd/goimports@v0.31.0
go install github.com/mgechev/revive@v1.9.0
go install github.com/gogf/gf/cmd/gf/v2@v2.10.4

git config --local core.hooksPath .githooks
echo 'Dev tools installed; Git hooks enabled for this repository. A failing check blocks the commit.'
