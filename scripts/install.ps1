# Install REX for everyday use: a private venv, `rex` on PATH, Cursor hook.
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
#   .\scripts\install.ps1     # from a clone, installs this tree
$ErrorActionPreference = "Stop"

$RepoUrl = if ($env:REX_REPO) { $env:REX_REPO } else { "https://github.com/luciancheng/rex.git" }
$Prefix = if ($env:REX_HOME) { $env:REX_HOME } else { Join-Path $HOME ".rex" }
$Venv = Join-Path $Prefix "venv"
$Scripts = Join-Path $Venv "Scripts"

function Find-Python {
    foreach ($name in @("py", "python", "python3")) {
        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if (-not $cmd) { continue }
        $exe = $cmd.Source
        $ok = & $exe -c "import sys; raise SystemExit(0 if sys.version_info >= (3, 9) else 1)" 2>$null
        if ($LASTEXITCODE -eq 0) { return $exe }
    }
    throw "rex install: need Python 3.9 or newer on PATH"
}

function Install-Src {
    if ($PSCommandPath -and -not $env:REX_FROM_GIT) {
        $here = Split-Path -Parent $PSCommandPath
        $root = Split-Path -Parent $here
        if (Test-Path (Join-Path $root "pyproject.toml")) {
            return $root
        }
    }
    return "git+$RepoUrl"
}

$Python = Find-Python
$Src = Install-Src

Write-Host "installing REX into $Venv"
New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
& $Python -m venv $Venv
$VenvPython = Join-Path $Scripts "python.exe"
& $VenvPython -m pip install --upgrade pip | Out-Null
& $VenvPython -m pip install $Src

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not $userPath) { $userPath = "" }
if (";$userPath;" -notlike "*;$Scripts;*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$Scripts", "User")
}
$env:Path = "$Scripts;$env:Path"

$Rex = Join-Path $Scripts "rex.exe"
try {
    & $Rex ingest --install-cursor-hook
} catch {
    Write-Host "installed rex, but could not write ~/.cursor/hooks.json (ok if Cursor is not installed)"
}

Write-Host ""
Write-Host "REX is installed. Open a new terminal, then:"
Write-Host "  rex start"
