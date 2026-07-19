# Agent Linux Tools

Independent Linux-native Shell and File services for AI agents. Both expose JSON-RPC 2.0 over separate Unix domain sockets or stdio.

## Features

- Stateless `/bin/bash -lc` executions with separate stdout/stderr events
- Persistent PTY sessions with `cd`, environment state, stdin, resize, and interrupt support
- Timeout and cancellation of complete Linux process groups
- Bounded inline output with full spill logs under `/tmp/agent-shell`
- Concurrent execution/session limits and idle session reaping
- Language-neutral JSON-RPC protocol plus an optional Go client
- Docker-first, non-root runtime
- Independent File Tool on `/run/agent/file-tool.sock`
- Workspace-confined file queries, exact edits, atomic batches, dry-run diffs, and rollback
- SHA-256 concurrency checks and symlink escape protection

## Build and run

```bash
go test ./...
go build -o bin/shell-tool ./cmd/shell-tool
./bin/shell-tool --transport unix --socket /tmp/shell-tool.sock
```

Or use Docker:

```bash
docker build -t agent-shell-tool .
docker run --rm -v "$PWD:/workspace" agent-shell-tool
```

For local protocol debugging:

```bash
SHELL_TOOL_TRANSPORT=stdio go run ./cmd/shell-tool
```

Each JSON-RPC message must occupy one line. See `docs/protocol.md` for method and event examples.

The File Tool is built separately and never invokes the Shell Tool:

```bash
go build -o bin/file-tool ./cmd/file-tool
go build -o bin/file-tool-console ./cmd/file-tool-console
./bin/file-tool --socket /tmp/file-tool.sock --workspace /tmp/file-workspace
```

See `docs/file-protocol.md` for the complete File Tool interface.

## Interactive test console

Start the service in one Linux/WSL terminal:

```bash
go run ./cmd/shell-tool --socket /tmp/shell-tool.sock
```

Start the interactive console in a second terminal:

```bash
go run ./cmd/shell-tool-console --socket /tmp/shell-tool.sock
```

The console accepts friendly commands and complete JSON parameter objects:

```text
health
exec pwd && id
exec-json {"command":"printf hi","cwd":"/tmp","timeout_ms":5000}
open dev /workspace
run dev export MODE=test; cd /tmp
run dev printf '%s:%s\n' "$MODE" "$PWD"
run-bg dev cat
write dev hello
write-json {"session_id":"dev","data_base64":"Cg=="}
interrupt dev
close dev
```

Use `help` inside the console for all commands. `exec-json`, `open-json`, `run-json`, `write-json`, and `call` allow direct manual testing of the JSON-RPC parameters.

### PowerShell + WSL

From PowerShell, start the WSL server in a new window and open the interactive console with one command:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl.ps1
```

You can also start both sides separately:

```powershell
.\scripts\start-wsl-server.ps1
.\scripts\start-wsl-console.ps1
```

The scripts default to `Ubuntu-24.04` and `/tmp/shell-tool.sock`. Override them with `-Distro` or `-Socket` when needed.

## Interactive File Tool test

From PowerShell, run the independent File Tool server and interactive console:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-file.ps1
```

The default test workspace is `/tmp/agent-file-tool-workspace`, so manual tests do not modify this repository. At the `file-tool>` prompt, enter:

```text
create-json {"path":"demo.txt","content":"hello world\n","create_parents":true}
read demo.txt
replace-json {"path":"demo.txt","old_text":"hello","new_text":"updated","expected_occurrences":1}
stat demo.txt
list . 2
rollback filetx-...
```

Copy the `transaction_id` returned by `replace-json` into the `rollback` command. `replace-json`, `copy-json`, `move-json`, `delete-json`, and `chmod-json` automatically query the current SHA-256 value for convenient manual testing. Use `help` for all commands and `call <method> <json>` to send arbitrary parameters.

## Configuration

| Environment variable | Default |
| --- | --- |
| `SHELL_TOOL_TRANSPORT` | `unix` |
| `SHELL_TOOL_SOCKET` | `/run/agent/shell-tool.sock` |
| `SHELL_TOOL_DEFAULT_SHELL` | `/bin/bash` |
| `SHELL_TOOL_DEFAULT_TIMEOUT` | `10m` |
| `SHELL_TOOL_KILL_GRACE` | `3s` |
| `SHELL_TOOL_MAX_EXEC` | `8` |
| `SHELL_TOOL_MAX_SESSIONS` | `4` |
| `SHELL_TOOL_SESSION_IDLE_TTL` | `15m` |
| `SHELL_TOOL_SESSION_DETACH_TTL` | `30s` |
| `SHELL_TOOL_SHUTDOWN_GRACE` | `5s` |
| `SHELL_TOOL_OUTPUT_LIMIT_BYTES` | `1048576` |
| `SHELL_TOOL_MAX_MESSAGE_BYTES` | `4194304` |
| `SHELL_TOOL_TEMP_DIR` | `/tmp/agent-shell` |

File Tool configuration uses the same independent pattern:

| Environment variable | Default |
| --- | --- |
| `FILE_TOOL_TRANSPORT` | `unix` |
| `FILE_TOOL_SOCKET` | `/run/agent/file-tool.sock` |
| `FILE_TOOL_WORKSPACE` | `/workspace` |
| `FILE_TOOL_TEMP_DIR` | `/tmp/agent-file-tool` |
| `FILE_TOOL_MAX_FILE_BYTES` | `8388608` |
| `FILE_TOOL_MAX_READ_BYTES` | `262144` |
| `FILE_TOOL_MAX_TRANSACTION_FILES` | `100` |
| `FILE_TOOL_MAX_TRANSACTION_BYTES` | `67108864` |
| `FILE_TOOL_MAX_CONCURRENT` | `8` |
| `FILE_TOOL_JOURNAL_TTL` | `15m` |

Docker is the primary security boundary. The service intentionally does not parse command semantics or maintain a command blacklist; callers can replace the default allow-all `ExecutionPolicy` when embedding the server.
