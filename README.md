# Agent Shell Tool

Linux-native shell execution service for AI agents. It exposes JSON-RPC 2.0 over a Unix domain socket or stdio, streams command output, manages Linux process groups, and supports persistent Bash PTY sessions.

## Features

- Stateless `/bin/bash -lc` executions with separate stdout/stderr events
- Persistent PTY sessions with `cd`, environment state, stdin, resize, and interrupt support
- Timeout and cancellation of complete Linux process groups
- Bounded inline output with full spill logs under `/tmp/agent-shell`
- Concurrent execution/session limits and idle session reaping
- Language-neutral JSON-RPC protocol plus an optional Go client
- Docker-first, non-root runtime

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

Docker is the primary security boundary. The service intentionally does not parse command semantics or maintain a command blacklist; callers can replace the default allow-all `ExecutionPolicy` when embedding the server.
