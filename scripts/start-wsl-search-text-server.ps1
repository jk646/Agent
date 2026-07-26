param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/search-text-tool.sock",
    [string]$Workspace = ""
)

$ErrorActionPreference = "Stop"
$repoPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$wslInputPath = $repoPath.Replace("\", "/")
$linuxRepo = (& wsl.exe -d $Distro -u root -- wslpath -a $wslInputPath).Trim()
if ($LASTEXITCODE -ne 0 -or -not $linuxRepo) {
    throw "Unable to resolve the repository path in WSL."
}
if (-not $Workspace) { $Workspace = $linuxRepo }
& wsl.exe -d $Distro -u root -- test -d $Workspace
if ($LASTEXITCODE -ne 0) { throw "Search Text Tool workspace does not exist: $Workspace" }
& wsl.exe -d $Distro -u root --cd $linuxRepo -- /usr/local/go/bin/go run ./cmd/search-text-tool --socket $Socket --workspace $Workspace
exit $LASTEXITCODE
