param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Workspace = "/tmp/agent-orchestrator-workspace",
    [string]$Socket = "/tmp/agent-orchestrator/orchestrator.sock"
)

$ErrorActionPreference = "Stop"
$stackScript = Join-Path $PSScriptRoot "start-wsl-orchestrator-stack.ps1"
$consoleScript = Join-Path $PSScriptRoot "start-wsl-orchestrator-console.ps1"
$arguments = "-NoExit -ExecutionPolicy Bypass -File `"$stackScript`" -Distro `"$Distro`" -Workspace `"$Workspace`""
Start-Process powershell.exe -ArgumentList $arguments

$ready = $false
for ($attempt = 0; $attempt -lt 600; $attempt++) {
    & wsl.exe -d $Distro -u root -- test -S $Socket
    if ($LASTEXITCODE -eq 0) { $ready = $true; break }
    Start-Sleep -Milliseconds 100
}
if (-not $ready) { throw "agent-orchestrator did not create $Socket within 60 seconds." }
& $consoleScript -Distro $Distro -Socket $Socket
exit $LASTEXITCODE
