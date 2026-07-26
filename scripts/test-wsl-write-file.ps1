param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/write-file-tool.sock",
    [string]$Workspace = "/tmp/agent-write-file-workspace",
    [string]$TempDir = "/tmp/agent-write-file-journal"
)

$ErrorActionPreference = "Stop"
$serverScript = Join-Path $PSScriptRoot "start-wsl-write-file-server.ps1"
$consoleScript = Join-Path $PSScriptRoot "start-wsl-write-file-console.ps1"
$arguments = "-NoExit -ExecutionPolicy Bypass -File `"$serverScript`" -Distro `"$Distro`" -Socket `"$Socket`" -Workspace `"$Workspace`" -TempDir `"$TempDir`""
Start-Process powershell.exe -ArgumentList $arguments
$ready = $false
for ($attempt = 0; $attempt -lt 150; $attempt++) {
    & wsl.exe -d $Distro -u root -- test -S $Socket
    if ($LASTEXITCODE -eq 0) { $ready = $true; break }
    Start-Sleep -Milliseconds 100
}
if (-not $ready) { throw "write-file-tool did not create $Socket within 15 seconds." }
& $consoleScript -Distro $Distro -Socket $Socket
exit $LASTEXITCODE
