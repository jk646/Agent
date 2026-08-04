#!/bin/sh
set -eu

repo=${1:?repository path is required}
workspace=${2:-/tmp/agent-orchestrator-workspace}
runtime=/tmp/agent-orchestrator
go=/usr/local/go/bin/go
pids=""

mkdir -p "$runtime" "$workspace" "$runtime/file-edit-journal" "$runtime/write-file-journal"

start() {
    "$@" &
    pids="$pids $!"
}

cleanup() {
    for pid in $pids; do
        kill "$pid" 2>/dev/null || true
    done
    wait 2>/dev/null || true
    rm -f "$runtime"/*.sock
}

trap cleanup EXIT INT TERM
cd "$repo"

start "$go" run ./cmd/shell-tool --socket "$runtime/shell.sock"
FILE_TOOL_TEMP_DIR="$runtime/file-edit-journal"
export FILE_TOOL_TEMP_DIR
start "$go" run ./cmd/file-tool --socket "$runtime/file-edit.sock" --workspace "$workspace"
unset FILE_TOOL_TEMP_DIR
start "$go" run ./cmd/file-search-tool --socket "$runtime/file-search.sock" --workspace "$workspace"
start "$go" run ./cmd/read-file-tool --socket "$runtime/read-file.sock" --workspace "$workspace"
start "$go" run ./cmd/read-folder-tool --socket "$runtime/read-folder.sock" --workspace "$workspace"
start "$go" run ./cmd/search-text-tool --socket "$runtime/search-text.sock" --workspace "$workspace"
start "$go" run ./cmd/write-file-tool --socket "$runtime/write-file.sock" --workspace "$workspace" --temp-dir "$runtime/write-file-journal"

for socket in shell file-edit file-search read-file read-folder search-text write-file; do
    attempts=0
    while [ ! -S "$runtime/$socket.sock" ]; do
        attempts=$((attempts + 1))
        if [ "$attempts" -ge 300 ]; then
            echo "Tool did not create $runtime/$socket.sock" >&2
            exit 1
        fi
        sleep 0.1
    done
done

start "$go" run ./cmd/agent-orchestrator --socket "$runtime/orchestrator.sock" --tools-config "$repo/configs/orchestrator-tools.wsl.json"
wait
