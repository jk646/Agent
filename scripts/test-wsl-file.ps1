param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/file-tool.sock",
    [string]$Workspace = "/tmp/agent-file-tool-workspace"
)

$ErrorActionPreference = "Stop"
$serverScript = Join-Path $PSScriptRoot "start-wsl-file-server.ps1"
$consoleScript = Join-Path $PSScriptRoot "start-wsl-file-console.ps1"
$arguments = "-NoExit -ExecutionPolicy Bypass -File `"$serverScript`" -Distro `"$Distro`" -Socket `"$Socket`" -Workspace `"$Workspace`""

Start-Process powershell.exe -ArgumentList $arguments

$ready = $false
for ($attempt = 0; $attempt -lt 150; $attempt++) {
    & wsl.exe -d $Distro -u root -- test -S $Socket
    if ($LASTEXITCODE -eq 0) {
        $ready = $true
        break
    }
    Start-Sleep -Milliseconds 100
}

if (-not $ready) {
    throw "file-tool did not create $Socket within 15 seconds."
}

Write-Host "File Tool workspace: $Workspace"
& $consoleScript -Distro $Distro -Socket $Socket
exit $LASTEXITCODE
