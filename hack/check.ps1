# Windows entry point. hack/check.sh runs the same checks in the same order on *nix.
# Keep this file pure ASCII: Windows PowerShell 5.1 decodes BOM-less files as ANSI,
# so non-ASCII text would corrupt quoting and break the parser.
$ErrorActionPreference = 'Stop'
Push-Location (Split-Path $PSScriptRoot -Parent)
try {
    $goimports = Join-Path (Get-Location) 'bin/tools/goimports.exe'
    $revive = Join-Path (Get-Location) 'bin/tools/revive.exe'
    if (-not (Test-Path -LiteralPath $goimports) -or -not (Test-Path -LiteralPath $revive)) {
        throw 'Missing dev tools. Run ./hack/install-tools.ps1 first, then retry.'
    }
    $moduleName = & go list -m
    if ($LASTEXITCODE -ne 0) { throw 'Failed to read the Go module name.' }
    $root = (Get-Location).Path
    # Generated code is excluded from import-style and GoDoc checks but still compiled and tested.
    $goFiles = @(Get-Item main.go) + @(Get-ChildItem -Path api,internal,hack -Recurse -Filter *.go)
    $scannedFiles = @($goFiles | Where-Object {
        $_.FullName -notmatch '[\\/]internal[\\/](dao|model[\\/](do|entity))[\\/]'
    })
    $sourceFiles = @($scannedFiles | ForEach-Object { $_.FullName })
    $unformatted = & $goimports -l -local $moduleName @sourceFiles
    if ($LASTEXITCODE -ne 0) { throw 'goimports failed.' }
    if ($unformatted) { throw "Run goimports on: $($unformatted -join ', ')" }
    # Revive v1.9 -exclude supports neither ** globs nor forward slashes on Windows, so
    # excluding tests through -exclude '**/*_test.go' silently did nothing. Build the
    # exclusion list explicitly from the scanned files instead. The paths must go to
    # -exclude rather than as positional arguments: a bare file argument is treated as
    # its own single-file package, which turns package-comments into a false positive
    # for every file that has no package doc of its own.
    $testFiles = @($scannedFiles | Where-Object { $_.Name -match '_test\.go$' })
    $lintCount = $scannedFiles.Count - $testFiles.Count
    if ($lintCount -eq 0) { throw 'No Go files left to lint; the file list is broken.' }
    Write-Host "lint files: $lintCount (test files excluded: $($testFiles.Count))"
    $reviveArgs = @('-config', 'hack/revive.toml',
        '-exclude', './internal/dao/...', '-exclude', './internal/model/do/...', '-exclude', './internal/model/entity/...')
    foreach ($file in $testFiles) {
        $reviveArgs += @('-exclude', $file.FullName.Substring($root.Length + 1))
    }
    & $revive @reviveArgs ./...
    if ($LASTEXITCODE -ne 0) { throw 'Revive reported naming, GoDoc or style issues.' }
    & go run ./hack/quality
    if ($LASTEXITCODE -ne 0) { throw 'Template convention check failed.' }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed.' }
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'go test failed.' }
    & go build -o bin/backend-template-go.exe .
    if ($LASTEXITCODE -ne 0) { throw 'go build failed.' }
} finally {
    Pop-Location
}
