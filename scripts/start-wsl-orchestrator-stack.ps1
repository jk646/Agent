param(
    [string]$Distro = "Ubuntu-24.04",
    [string]$Workspace = "/tmp/agent-orchestrator-workspace"
)

$ErrorActionPreference = "Stop"
$repoPath = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$linuxRepo = (& wsl.exe -d $Distro -u root -- wslpath -a $repoPath.Replace("\", "/")).Trim()
if ($LASTEXITCODE -ne 0 -or -not $linuxRepo) { throw "Unable to resolve the repository path in WSL." }
& wsl.exe -d $Distro -u root --cd $linuxRepo -- /bin/sh ./scripts/start-wsl-tool-stack.sh $linuxRepo $Workspace
exit $LASTEXITCODE
