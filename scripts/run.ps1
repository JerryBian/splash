<#
.SYNOPSIS
    splash one-click build + start + demo data (Windows / PowerShell).

.DESCRIPTION
    Flow: locate Go/Node/pnpm -> force-clear obstacles (stop old splash
    instances and whatever holds the port) -> node scripts/build.mjs (frontend
    build + go:embed + binary) -> prepare the demo database -> start
    bin\splash.exe -seed -> wait for readiness -> open the browser.

    Force-start: when the port or a process is occupied, the occupier is
    stopped (system-critical processes are refused with a hint to change
    -Port); the script never refuses to start because of an old instance.

    Demo data lives in a separate data/demo.db, recreated on every run so each
    start gets fresh sample data; your real database (data/splash.db by
    default) is never deleted.

.PARAMETER Port
    Listen port, default 8080 (bound to 127.0.0.1, local only).

.PARAMETER SkipBuild
    Skip compilation and start the existing bin\splash.exe (do not use after
    frontend changes).

.PARAMETER KeepData
    Keep the previous demo data (do not recreate demo.db; your check-ins
    survive the restart).

.PARAMETER DbPath
    Use a specific database file. Never deleted by this script; -seed only
    fills it when empty.

.PARAMETER Foreground
    Run the server in this console (Ctrl+C stops it); no new window and no
    automatic browser launch.

.EXAMPLE
    pwsh -File scripts/run.ps1                 # build + start + fresh demo data + browser
    pwsh -File scripts/run.ps1 -SkipBuild      # quick restart, skip the build
    pwsh -File scripts/run.ps1 -KeepData       # keep the previous demo data
    pwsh -File scripts/run.ps1 -Port 9000 -Foreground

.NOTES
    If the execution policy blocks scripts:
        pwsh -ExecutionPolicy Bypass -File scripts/run.ps1
    Admin account defaults to admin / splash123; override with
    SPLASH_ADMIN_USERNAME / SPLASH_ADMIN_PASSWORD / SPLASH_AUTH_SECRET
    (the current environment is passed through to the server).
#>
[CmdletBinding()]
param(
    [int]$Port = 8080,
    [switch]$SkipBuild,
    [switch]$KeepData,
    [string]$DbPath = '',
    [switch]$Foreground
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Write-Step([string]$Message) { Write-Host "`n==> $Message" -ForegroundColor Cyan }
function Write-Ok([string]$Message)   { Write-Host "    $Message" -ForegroundColor Green }
function Die([string]$Message)        { Write-Host "`n[run] failed: $Message" -ForegroundColor Red; exit 1 }

Write-Host 'splash - one-click build + start + demo data' -ForegroundColor Magenta

# ---------------------------------------------------------------- toolchain
Write-Step 'Checking the toolchain'

$goCmd = Get-Command go -ErrorAction SilentlyContinue
if ($goCmd) {
    $goExe = $goCmd.Source
} else {
    # Go installed but not on PATH is common on this machine; probe the
    # usual install locations.
    $candidates = @(
        "$env:ProgramFiles\Go\bin\go.exe",
        'C:\Go\bin\go.exe',
        "$env:USERPROFILE\go\bin\go.exe",
        "$env:USERPROFILE\scoop\apps\go\current\bin\go.exe",
        "$env:LOCALAPPDATA\Programs\Go\bin\go.exe"
    )
    $goExe = $candidates | Where-Object { Test-Path $_ } | Select-Object -First 1
}
if (-not $goExe) { Die 'Go 1.27+ not found. Install it, or add go.exe to PATH.' }
$env:GO = $goExe
$env:PATH = (Split-Path -Parent $goExe) + ';' + $env:PATH
Write-Ok "go:    $goExe"

$nodeCmd = Get-Command node -ErrorAction SilentlyContinue
if (-not $nodeCmd) { Die 'Node.js not found (need 20.19+ or 22.12+).' }
Write-Ok "node:  $($nodeCmd.Source)"

$pnpmCmd = Get-Command pnpm -ErrorAction SilentlyContinue
if (-not $pnpmCmd) { Die 'pnpm not found. Run: npm install -g pnpm (or corepack enable on Node < 25).' }
Write-Ok "pnpm:  $($pnpmCmd.Source)"

# ---------------------------------------------------------------- obstacles
# Force-start: stop old splash instances first (they may hold demo.db), then
# whatever listens on the port, then wait for the port to free up. Only after
# that is the database touched. System-critical processes are never killed.
$base = "http://127.0.0.1:$Port"
Write-Step 'Force-clearing obstacles: old instances and port occupants'

Get-Process -Name splash -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Ok "Stopping old splash instance PID $($_.Id)"
    Stop-Process -Id $_.Id -Force -Confirm:$false
}

$protectedNames = @('system', 'registry', 'smss', 'csrss', 'wininit', 'services', 'lsass', 'memory compression', 'idle')
$ownerPids = @(Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue |
    Select-Object -ExpandProperty OwningProcess -Unique)
foreach ($ownerPid in $ownerPids) {
    if ($ownerPid -le 4) {
        Die "Port $Port is held by a system process (PID $ownerPid); cannot force-kill. Use another port: -Port 9000."
    }
    $owner = Get-Process -Id $ownerPid -ErrorAction SilentlyContinue
    if (-not $owner) { continue }
    if ($protectedNames -contains $owner.Name.ToLower()) {
        Die "Port $Port is held by system-critical process $($owner.Name) (PID $ownerPid); refusing to kill. Use another port: -Port 9000."
    }
    Write-Ok "Stopping port occupant $($owner.Name) (PID $ownerPid)"
    Stop-Process -Id $ownerPid -Force -Confirm:$false
}

# Process exit and port release are not simultaneous; wait for the latter.
Start-Sleep -Milliseconds 500
foreach ($i in 1..20) {
    $still = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    if (-not $still) { break }
    Start-Sleep -Milliseconds 250
}
if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
    Die "Port $Port is still occupied after clearing. Use another port: -Port 9000."
}
Write-Ok "Port $Port is free"

# ---------------------------------------------------------------- database
# Default is a separate demo database, recreated each run; only the demo.db
# this script owns is ever deleted.
$isDemoDb = ($DbPath -eq '')
if ($isDemoDb) { $DbPath = Join-Path $root 'data\demo.db' }
if (-not [System.IO.Path]::IsPathRooted($DbPath)) { $DbPath = Join-Path $root $DbPath }

if ($isDemoDb -and -not $KeepData) {
    Write-Step 'Recreating the demo database (fresh sample data)'
    foreach ($suffix in @('', '-wal', '-shm')) {
        $file = $DbPath + $suffix
        if (Test-Path $file) {
            try {
                Remove-Item $file -Force -Confirm:$false
                Write-Ok "Removed $file"
            } catch {
                Die "Cannot delete $file (held by another process?). Stop that server first, or use -KeepData."
            }
        }
    }
} else {
    Write-Step "Using database $DbPath (seeded only when empty)"
}

# ---------------------------------------------------------------- build
$exe = Join-Path $root 'bin\splash.exe'
if ($SkipBuild) {
    Write-Step 'Skipping the build (-SkipBuild)'
    if (-not (Test-Path $exe)) { Die 'bin\splash.exe does not exist; run once without -SkipBuild first.' }
} else {
    Write-Step 'Building: frontend -> go:embed -> binary (about a minute on first run)'
    & node scripts/build.mjs
    if ($LASTEXITCODE -ne 0) { Die "Build failed (exit $LASTEXITCODE); see the build log above." }
    if (-not (Test-Path $exe)) { Die 'Build finished but bin\splash.exe is missing.' }
}
Write-Ok "Binary: $exe"

# ---------------------------------------------------------------- start
$serverArgs = @('-seed', '-http-addr', "127.0.0.1:$Port", '-db-path', "`"$DbPath`"")
$username = if ($env:SPLASH_ADMIN_USERNAME) { $env:SPLASH_ADMIN_USERNAME } else { 'admin' }
$password = if ($env:SPLASH_ADMIN_PASSWORD) { $env:SPLASH_ADMIN_PASSWORD } else { 'splash123' }

if ($Foreground) {
    Write-Step "Starting in the foreground at $base (Ctrl+C stops)"
    Write-Host "    Account: $username / $password    Database: $DbPath" -ForegroundColor Yellow
    & $exe @serverArgs
    Write-Host "`n[run] Server stopped." -ForegroundColor Magenta
    exit 0
}

Write-Step 'Starting the server (separate window; close it to stop)'
$proc = Start-Process -FilePath $exe -ArgumentList $serverArgs -WorkingDirectory $root -PassThru
Write-Ok "Process PID: $($proc.Id)"

# Readiness wait (the seed inserts ~1500 rows in one transaction, usually <1s).
$ready = $false
foreach ($i in 1..60) {
    Start-Sleep -Milliseconds 500
    if ($proc.HasExited) { Die "Server process exited early (code $($proc.ExitCode)). Run with -Foreground to see the log." }
    try {
        $health = Invoke-RestMethod -Uri "$base/api/v1/health" -TimeoutSec 2
        if ($health.status -eq 'ok') { $ready = $true; break }
    } catch { }
}
if (-not $ready) { Die 'Server not ready within 30s. Run with -Foreground to see the log.' }

# Confirm the demo data landed, then open the browser.
$projectCount = 0
try {
    $projects = Invoke-RestMethod -Uri "$base/api/v1/projects" -TimeoutSec 5
    $projectCount = $projects.projects.Count
} catch { }
Start-Process $base

Write-Host ''
Write-Host '  OK splash is ready' -ForegroundColor Green
Write-Host "     URL:      $base (browser opened)"
Write-Host "     Account:  $username / $password"
Write-Host "     Demo data: $projectCount public projects + ~120 days of history (private ones visible after login)"
Write-Host "     Database: $DbPath"
Write-Host '     Stop:     close the server window, or Stop-Process -Id ' -NoNewline
Write-Host "$($proc.Id)" -ForegroundColor Yellow
Write-Host ''
Write-Host '  Next quick start (skip build, keep data):' -ForegroundColor DarkGray
Write-Host '     pwsh -File scripts/run.ps1 -SkipBuild -KeepData' -ForegroundColor DarkGray
