# Repoints the LatticeAgent scheduled task to launch via a hidden wscript wrapper,
# so the agent no longer pops a visible console window on (re)launch.
# RUN AS ADMINISTRATOR (the task runs at RunLevel=Highest, so editing it requires elevation).
#
#   Right-click this file > "Run with PowerShell" as admin, OR from an elevated terminal:
#     powershell -ExecutionPolicy Bypass -File "<path-to-this-file>"
#
# Idempotent + reversible. To revert: set the action back to lattice.exe with the agent args.

$ErrorActionPreference = 'Stop'

$vbs = Join-Path $env:LOCALAPPDATA 'Lattice\run-agent-hidden.vbs'
if (-not (Test-Path $vbs)) {
    throw "Hidden launcher not found at $vbs - recreate it first."
}

$t = Get-ScheduledTask -TaskName 'LatticeAgent'
$action = New-ScheduledTaskAction -Execute 'wscript.exe' -Argument "`"$vbs`""
Set-ScheduledTask -TaskName 'LatticeAgent' -Action $action -Trigger $t.Triggers -Settings $t.Settings -Principal $t.Principal | Out-Null

# Replace the currently-running (visible) instance with a hidden one
Stop-ScheduledTask  -TaskName 'LatticeAgent'
Start-Sleep -Seconds 1
Start-ScheduledTask -TaskName 'LatticeAgent'
Start-Sleep -Seconds 2

$a = (Get-ScheduledTask -TaskName 'LatticeAgent').Actions
$running = [bool](Get-Process lattice -ErrorAction SilentlyContinue)
Write-Host "Done. Task action -> $($a.Execute) $($a.Arguments)"
Write-Host "lattice.exe running (hidden): $running"
