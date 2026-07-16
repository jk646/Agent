param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/shell-tool.sock"
)

$ErrorActionPreference = "Stop"
$repoPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$wslInputPath = $repoPath.Replace("\", "/")
$linuxRepo = (& wsl.exe -d $Distro -u root -- wslpath -a $wslInputPath).Trim()
if ($LASTEXITCODE -ne 0 -or -not $linuxRepo) {
    throw "Unable to resolve the repository path in WSL."
}

& wsl.exe -d $Distro -u root --cd $linuxRepo -- /usr/local/go/bin/go run ./cmd/shell-tool-console --socket $Socket
exit $LASTEXITCODE
