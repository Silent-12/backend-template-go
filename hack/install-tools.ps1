# Install the pinned dev tools into bin/tools and enable this repository's Git hooks.
# hack/install-tools.sh installs the same versions on *nix; keep both lists identical.
# Keep this file pure ASCII: Windows PowerShell 5.1 decodes BOM-less files as ANSI,
# so non-ASCII text would corrupt quoting and break the parser.
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path $PSScriptRoot -Parent
$repositoryRoot = & git -C $projectRoot rev-parse --show-toplevel
if ($LASTEXITCODE -ne 0) { throw 'Run this script from a cloned Git repository.' }
if ([IO.Path]::GetFullPath($repositoryRoot) -ne [IO.Path]::GetFullPath($projectRoot)) {
    throw 'The project must be its own Git repository; refusing to change the parent repository hooks.'
}
if (-not (Test-Path -LiteralPath (Join-Path $projectRoot '.githooks/pre-commit') -PathType Leaf)) {
    throw 'Missing .githooks/pre-commit; cannot enable the pre-commit hook.'
}
$previousToolsBin = $env:GOBIN
try {
    $env:GOBIN = Join-Path $projectRoot 'bin/tools'
    foreach ($tool in @(
        'golang.org/x/tools/cmd/goimports@v0.31.0',
        'github.com/mgechev/revive@v1.9.0',
        'github.com/gogf/gf/cmd/gf/v2@v2.10.4'
    )) {
        & go install $tool
        if ($LASTEXITCODE -ne 0) { throw "Failed to install dev tool: $tool" }
    }
    & git -C $projectRoot config --local core.hooksPath .githooks
    if ($LASTEXITCODE -ne 0) { throw 'Failed to enable Git hooks for this repository.' }
    Write-Host 'Dev tools installed; Git hooks enabled for this repository. A failing check blocks the commit.'
} finally {
    $env:GOBIN = $previousToolsBin
}
