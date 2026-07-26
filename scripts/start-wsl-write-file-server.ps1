param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Socket = "/tmp/write-file-tool.sock",
    [string]$Workspace = "/tmp/agent-write-file-workspace",
    [string]$TempDir = "/tmp/agent-write-file-journal"
)

$ErrorActionPreference = "Stop"
$repoPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$linuxRepo = (& wsl.exe -d $Distro -u root -- wslpath -a $repoPath.Replace("\", "/")).Trim()
if ($LASTEXITCODE -ne 0 -or -not $linuxRepo) { throw "Unable to resolve the repository path in WSL." }
& wsl.exe -d $Distro -u root -- mkdir -p $Workspace $TempDir
if ($LASTEXITCODE -ne 0) { throw "Unable to prepare the Write File Tool workspace." }
Write-Host "Write File Tool workspace: $Workspace"
& wsl.exe -d $Distro -u root --cd $linuxRepo -- /usr/local/go/bin/go run ./cmd/write-file-tool --socket $Socket --workspace $Workspace --temp-dir $TempDir
exit $LASTEXITCODE
