param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/shell-tool.sock"
)

$ErrorActionPreference = "Stop"
$serverScript = Join-Path $PSScriptRoot "start-wsl-server.ps1"
$consoleScript = Join-Path $PSScriptRoot "start-wsl-console.ps1"
$arguments = "-NoExit -ExecutionPolicy Bypass -File `"$serverScript`" -Distro `"$Distro`" -Socket `"$Socket`""

Start-Process powershell.exe -ArgumentList $arguments

$ready = $false
for ($attempt = 0; $attempt -lt 100; $attempt++) {
    & wsl.exe -d $Distro -u root -- test -S $Socket
    if ($LASTEXITCODE -eq 0) {
        $ready = $true
        break
    }
    Start-Sleep -Milliseconds 100
}

if (-not $ready) {
    throw "shell-tool did not create $Socket within 10 seconds."
}

& $consoleScript -Distro $Distro -Socket $Socket
exit $LASTEXITCODE
