param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/file-search-tool.sock",
    [string]$Workspace = ""
)

$ErrorActionPreference = "Stop"
$serverScript = Join-Path $PSScriptRoot "start-wsl-file-search-server.ps1"
$consoleScript = Join-Path $PSScriptRoot "start-wsl-file-search-console.ps1"
$arguments = "-NoExit -ExecutionPolicy Bypass -File `"$serverScript`" -Distro `"$Distro`" -Socket `"$Socket`""
if ($Workspace) {
    $arguments += " -Workspace `"$Workspace`""
}

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
    throw "file-search-tool did not create $Socket within 15 seconds."
}

& $consoleScript -Distro $Distro -Socket $Socket
exit $LASTEXITCODE
