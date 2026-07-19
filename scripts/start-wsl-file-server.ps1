param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/file-tool.sock",
    [string]$Workspace = "/tmp/agent-file-tool-workspace"
)

$ErrorActionPreference = "Stop"
$repoPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$wslInputPath = $repoPath.Replace("\", "/")
$linuxRepo = (& wsl.exe -d $Distro -u root -- wslpath -a $wslInputPath).Trim()
if ($LASTEXITCODE -ne 0 -or -not $linuxRepo) {
    throw "Unable to resolve the repository path in WSL."
}

& wsl.exe -d $Distro -u root -- mkdir -p $Workspace
if ($LASTEXITCODE -ne 0) {
    throw "Unable to create the File Tool workspace $Workspace."
}

& wsl.exe -d $Distro -u root --cd $linuxRepo -- /usr/local/go/bin/go run ./cmd/file-tool --socket $Socket --workspace $Workspace
exit $LASTEXITCODE
