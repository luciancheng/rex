# Install REX for everyday use: a Go binary on PATH, Cursor hook.
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
#   .\scripts\install.ps1     # from a clone, installs this tree
$ErrorActionPreference = "Stop"

$RepoUrl = if ($env:REX_REPO) { $env:REX_REPO } else { "https://github.com/luciancheng/rex.git" }
$Prefix = if ($env:REX_HOME) { $env:REX_HOME } else { Join-Path $HOME ".rex" }
$BinDir = if ($env:REX_BIN) { $env:REX_BIN } else { Join-Path $HOME ".local\bin" }

function Assert-Go {
    $cmd = Get-Command go -ErrorAction SilentlyContinue
    if (-not $cmd) {
        throw "rex install: need Go 1.22 or newer on PATH (https://go.dev/dl/)"
    }
}

function Local-Src {
    if ($PSCommandPath -and -not $env:REX_FROM_GIT) {
        $here = Split-Path -Parent $PSCommandPath
        $root = Split-Path -Parent $here
        if (Test-Path (Join-Path $root "go.mod")) {
            return $root
        }
    }
    return $null
}

Assert-Go
New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
$Rex = Join-Path $BinDir "rex.exe"

$Src = Local-Src
if ($Src) {
    Write-Host "building REX from $Src"
    Push-Location $Src
    try {
        go build -o $Rex ./cmd/rex
    } finally {
        Pop-Location
    }
} else {
    Write-Host "installing REX from $RepoUrl"
    $env:GOBIN = $BinDir
    try {
        go install "github.com/luciancheng/rex/cmd/rex@latest"
    } catch {
        $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("rex-" + [guid]::NewGuid().ToString())
        git clone --depth 1 $RepoUrl $tmp
        Push-Location $tmp
        try {
            go build -o $Rex ./cmd/rex
        } finally {
            Pop-Location
            Remove-Item -Recurse -Force $tmp
        }
    }
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not $userPath) { $userPath = "" }
if (";$userPath;" -notlike "*;$BinDir;*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$BinDir", "User")
}
$env:Path = "$BinDir;$env:Path"

try {
    & $Rex ingest --install-cursor-hook
} catch {
    Write-Host "installed rex, but could not write ~/.cursor/hooks.json (ok if Cursor is not installed)"
}

Write-Host ""
Write-Host "REX is installed. Open a new terminal, then:"
Write-Host "  rex start"
