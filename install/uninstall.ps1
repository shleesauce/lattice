# Lattice uninstaller (Windows). Completely removes Lattice from this machine
# and nothing else.
#
# What it does:
#   1. Stops + unregisters the Lattice scheduled task(s) — hub AND agent,
#      whichever this box ran: 'LatticeHub' / 'LatticeAgent'.
#   2. Stops any lingering lattice.exe process.
#   3. Deletes both Lattice directories:
#        - %LOCALAPPDATA%\Lattice   (binary + hidden-launch .vbs)
#        - %USERPROFILE%\.lattice   (config.json, SQLite database, token, logs)
#
# What it does NOT touch — by design: it runs as the current user (no admin),
# changes nothing outside Lattice's own paths, and leaves your files, Claude,
# and Tailscale / SSH / Syncthing exactly as they were.
#
# Usage:
#   irm https://raw.githubusercontent.com/shleesauce/lattice/master/install/uninstall.ps1 | iex
#   # preview without changing anything:
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/shleesauce/lattice/master/install/uninstall.ps1))) -DryRun
#
# Re-running is safe: it is idempotent and reports when there is nothing left.
param([switch]$DryRun)
$ErrorActionPreference = 'SilentlyContinue'

function Say($m) { Write-Host "lattice-uninstall: $m" }
$removedAny = $false
if ($DryRun) { Say 'DRY RUN - nothing will be changed' }

foreach ($task in 'LatticeHub', 'LatticeAgent') {
  if (Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue) {
    Say "stopping + unregistering scheduled task $task"
    if (-not $DryRun) {
      Stop-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue
      Unregister-ScheduledTask -TaskName $task -Confirm:$false -ErrorAction SilentlyContinue
    } else { Say "  would unregister $task" }
    $script:removedAny = $true
  }
}

Get-Process -Name 'lattice' -ErrorAction SilentlyContinue | ForEach-Object {
  Say "stopping process $($_.Id)"
  if (-not $DryRun) { $_ | Stop-Process -Force -ErrorAction SilentlyContinue }
  $script:removedAny = $true
}

# Remove the install dir from the user PATH if the installer added it.
$installDir = Join-Path $env:LOCALAPPDATA 'Lattice'
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -and ($userPath -like "*$installDir*")) {
  Say "removing $installDir from your user PATH"
  if (-not $DryRun) {
    $new = (($userPath -split ';') | Where-Object { $_ -and ($_ -ne $installDir) }) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $new, 'User')
  }
  $script:removedAny = $true
}

foreach ($dir in @($installDir, (Join-Path $env:USERPROFILE '.lattice'))) {
  if (Test-Path $dir) {
    Say "removing $dir"
    if (-not $DryRun) { Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue }
    else { Say "  would remove $dir" }
    $script:removedAny = $true
  }
}

if (-not $removedAny) {
  Say 'nothing to remove - Lattice was not installed for this user (already clean)'
} else {
  Say 'done - Lattice has been completely removed from this machine.'
}
if ($DryRun) { Say '(dry run - re-run without -DryRun to apply)' }
