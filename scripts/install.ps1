# Install REX: a Go binary on PATH, plus the Cursor hook when Cursor is here.
# Other agents are opt-in; the script prints the command for each one it finds.
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/luciancheng/rex/main/scripts/install.ps1 | iex
#   $env:REX_REF = "v0.2.0"; irm …/install.ps1 | iex      # a tag, branch or commit
#   .\scripts\install.ps1                                 # from a clone: installs this tree
$ErrorActionPreference = "Stop"

$Module = "github.com/luciancheng/rex"
$RepoUrl = if ($env:REX_REPO) { $env:REX_REPO } else { "https://$Module.git" }
$Ref = $env:REX_REF
$BinDir = if ($env:REX_BIN) { $env:REX_BIN } else { Join-Path $HOME ".local\bin" }

function Assert-Go {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "rex install: need Go 1.22 or newer on PATH (https://go.dev/dl/)"
    }
}

function Local-Src {
    if ($PSCommandPath -and -not $env:REX_FROM_GIT -and -not $Ref) {
        $root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
        if (Test-Path (Join-Path $root "go.mod")) { return $root }
    }
    return $null
}

# Fetches exactly $Ref (a tag, branch or commit) and builds it (a
# branch-only clone could not fetch a commit). Native commands don't
# throw, so each exit code is checked.
function Build-FromGit {
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("rex-" + [guid]::NewGuid().ToString())
    $want = if ($Ref) { $Ref } else { "HEAD" }
    try {
        # Output goes to Out-Null: in a function it would become the return value.
        git init -q $tmp | Out-Null; if ($LASTEXITCODE -ne 0) { return $false }
        git -C $tmp remote add origin $RepoUrl | Out-Null; if ($LASTEXITCODE -ne 0) { return $false }
        git -C $tmp fetch -q --depth 1 origin $want | Out-Null; if ($LASTEXITCODE -ne 0) { return $false }
        git -C $tmp checkout -q FETCH_HEAD | Out-Null; if ($LASTEXITCODE -ne 0) { return $false }
        Push-Location $tmp
        try { go build -o $Rex ./cmd/rex | Out-Null } finally { Pop-Location }
        return ($LASTEXITCODE -eq 0)
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

Assert-Go
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
$Rex = Join-Path $BinDir "rex.exe"

$Src = Local-Src
if ($Src) {
    Write-Host "building REX from $Src"
    Push-Location $Src
    try { go build -o $Rex ./cmd/rex } finally { Pop-Location }
    if ($LASTEXITCODE -ne 0) { throw "rex install: build failed" }
} else {
    $installed = $false
    if ($RepoUrl -eq "https://$Module.git" -or $RepoUrl -eq "https://$Module") {
        $at = if ($Ref) { $Ref } else { "latest" }
        Write-Host "installing $Module/cmd/rex@$at"
        $env:GOBIN = $BinDir
        go install "$Module/cmd/rex@$at"
        $installed = ($LASTEXITCODE -eq 0)
    }
    if (-not $installed) {
        Write-Host "building REX from $RepoUrl $(if ($Ref) { "at $Ref" })"
        if (-not (Build-FromGit)) { throw "rex install: could not fetch and build $(if ($Ref) { $Ref } else { 'the default branch' }) from $RepoUrl" }
    }
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not $userPath) { $userPath = "" }
if (";$userPath;" -notlike "*;$BinDir;*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$BinDir", "User")
}
$env:Path = "$BinDir;$env:Path"
& $Rex --version

# Cursor is the one agent wired by default, as before.
$hints = @()
if (Test-Path (Join-Path $HOME ".cursor")) {
    & $Rex ingest --install cursor
    if ($LASTEXITCODE -ne 0) { Write-Host "rex is installed, but the Cursor hook could not be written" }
} else {
    $hints += "rex ingest --install cursor        # once Cursor is installed"
}
$claude = if ($env:CLAUDE_CONFIG_DIR) { $env:CLAUDE_CONFIG_DIR } else { Join-Path $HOME ".claude" }
$codex = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME ".codex" }
$opencode = if ($env:XDG_CONFIG_HOME) { Join-Path $env:XDG_CONFIG_HOME "opencode" } else { Join-Path $HOME ".config\opencode" }
if (Test-Path $claude) { $hints += "rex ingest --install claude        # Claude Code (add --project for one repo)" }
if (Test-Path $codex) { $hints += "rex ingest --install codex         # Codex, then trust the hooks in /hooks" }
if (Test-Path $opencode) { $hints += "rex ingest --install opencode      # OpenCode" }
if ($hints.Count -gt 0) {
    Write-Host ""
    Write-Host "Other agents (opt-in):"
    foreach ($h in $hints) { Write-Host "  $h" }
    Write-Host "  rex ingest --install-skill         # teach agents the plan format"
}

Write-Host ""
Write-Host "REX is installed. Open a new terminal, then, in the repo you want to send off:"
Write-Host "  cd <your-repo>; rex start"
