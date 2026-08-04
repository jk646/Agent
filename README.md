# Agent Linux Tools

Independent Linux-native Shell, File Edit, File Search, Read File, Read Folder, Search Text, and Write File services for AI agents. All seven expose JSON-RPC 2.0 over separate Unix domain sockets or stdio.

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
- Independent read-only File Search Tool on `/run/agent/file-search-tool.sock`
- Ranked name/Glob search, metadata filters, content regex, context lines, pagination, and cancellation
- Independent Read File Tool on `/run/agent/read-file-tool.sock`
- UTF-8/UTF-16 text, line and character ranges, Base64 binary chunks, SHA-256, pagination, and cancellation
- Independent Read Folder Tool on `/run/agent/read-folder-tool.sock`
- Folder metadata, filtered lists, trees, summaries, snapshots, comparisons, pagination, and cancellation
- Independent Search Text Tool on `/run/agent/search-text-tool.sock`
- Literal, RE2 regular expression, multi-pattern, context, paths-only, counts, UTF-8/UTF-16, pagination, and cancellation
- Independent Write File Tool on `/run/agent/write-file-tool.sock`
- Atomic create, overwrite, append, offset writes, batches, previews, SHA-256 guards, and rollback
- Config-driven Agent Orchestrator on `/run/agent/orchestrator.sock`
- Dynamic Tool registration, prefix routing, health discovery, serial/parallel batches, hot reload, and Tool event forwarding

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

The File Search Tool is a third independent binary:

```bash
go build -o bin/file-search-tool ./cmd/file-search-tool
go build -o bin/file-search-tool-console ./cmd/file-search-tool-console
./bin/file-search-tool --socket /tmp/file-search-tool.sock --workspace /workspace
```

See `docs/file-search-protocol.md` for the complete search interface.

The Read File Tool is a fourth independent binary:

```bash
go build -o bin/read-file-tool ./cmd/read-file-tool
go build -o bin/read-file-tool-console ./cmd/read-file-tool-console
./bin/read-file-tool --socket /tmp/read-file-tool.sock --workspace /workspace
```

See `docs/read-file-protocol.md` for the complete read interface.

The Read Folder Tool is a fifth independent binary:

```bash
go build -o bin/read-folder-tool ./cmd/read-folder-tool
go build -o bin/read-folder-tool-console ./cmd/read-folder-tool-console
./bin/read-folder-tool --socket /tmp/read-folder-tool.sock --workspace /workspace
```

See `docs/read-folder-protocol.md` for the complete folder interface.

The Search Text Tool is a sixth independent binary and does not invoke Shell or system `grep`:

```bash
go build -o bin/search-text-tool ./cmd/search-text-tool
go build -o bin/search-text-tool-console ./cmd/search-text-tool-console
./bin/search-text-tool --socket /tmp/search-text-tool.sock --workspace /workspace
```

See `docs/search-text-protocol.md` for the complete text search interface.

The Write File Tool is a seventh independent binary. It writes complete content but does not perform text replacements or invoke another Tool:

```bash
go build -o bin/write-file-tool ./cmd/write-file-tool
go build -o bin/write-file-tool-console ./cmd/write-file-tool-console
./bin/write-file-tool --socket /tmp/write-file-tool.sock --workspace /workspace
```

See `docs/write-file-protocol.md` for its complete interface.

The Agent Orchestrator connects all Tool sockets without importing their business packages:

```bash
go build -o bin/agent-orchestrator ./cmd/agent-orchestrator
go build -o bin/agent-orchestrator-console ./cmd/agent-orchestrator-console
go build -o bin/agent-orchestrator-load ./cmd/agent-orchestrator-load
./bin/agent-orchestrator --tools-config ./configs/orchestrator-tools.json
```

Tool registration is configuration-driven. See `docs/orchestrator-protocol.md` for routing, batching, events, and adding future Tools.

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

## Interactive File Search Tool test

From PowerShell, start the independent read-only search server and console:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-file-search.ps1
```

By default the search workspace is this repository. At the `file-search-tool>` prompt, enter:

```text
find server . 20
glob *.go . 100
find-json {"path":".","name":"manager","type":"file","extensions":[".go"],"max_depth":8,"limit":50}
content-json {"path":".","query":"JSON-RPC","file_pattern":"*.go","context_before":1,"context_after":1,"limit":50}
```

Use `-Workspace /some/linux/path` to scan a different WSL directory. The search service is read-only and does not invoke the Shell Tool or File Edit Tool.

## Interactive Read File Tool test

From PowerShell, start the independent read server and console:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-read-file.ps1
```

By default the read workspace is this repository. At the `read-file-tool>` prompt, enter:

```text
stat README.md
read README.md 1 30
lines-json {"path":"README.md","start_line":10,"end_line":20,"include_line_numbers":true}
text-json {"path":"README.md","start_char":0,"end_char":200}
bytes README.md 0 64
hash README.md
```

Use `-Workspace /some/linux/path` to read another WSL directory. Binary data is returned as Base64; the service never modifies files or calls another Tool.

## Interactive Read Folder Tool test

From PowerShell, start the independent folder reader and console:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-read-folder.ps1
```

By default the workspace is this repository. At the `read-folder-tool>` prompt, enter:

```text
stat .
list internal 2 100
tree cmd 3
summary . 10
snapshot internal 3
list-json {"path":".","depth":3,"extensions":[".go"],"sort_by":"size","sort_order":"desc","limit":50}
```

The service reads folder structure and metadata only. File contents are not read unless a snapshot explicitly enables bounded file hashing.

## Interactive Search Text Tool test

From PowerShell, start the independent text search server and console in WSL:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-search-text.ps1
```

At the `search-text-tool>` prompt, enter:

```text
search TODO . 50
regex TODO|FIXME . 100
search-json {"path":".","query":"JSON-RPC","extensions":[".md"],"context_before":1,"context_after":1}
multi-json {"path":".","patterns":[{"id":"todo","query":"TODO"},{"id":"fixme","query":"FIXME"}],"extensions":["go"]}
files-json {"path":".","query":"package","include_patterns":["**/*.go"]}
count error internal 100
```

The service uses Go RE2 matching in-process, skips symlinks and binary files, and never calls another Tool.

## Interactive Write File Tool test

From PowerShell, start the independent writer and console in WSL:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-write-file.ps1
```

The default workspace is `/tmp/agent-write-file-workspace`. At the `write-file-tool>` prompt, enter:

```text
create-json {"path":"demo.txt","content":"hello\n","create_parents":true}
append-json {"path":"demo.txt","content":"next line","add_newline":true}
preview-json {"operations":[{"kind":"overwrite","path":"demo.txt","content":"preview only\n"}]}
overwrite-json {"path":"demo.txt","content":"updated\n"}
rollback writetx-...
```

Copy the returned `transaction_id` into `rollback`. Binary data uses `data_base64`.

## Interactive Orchestrator test

Start all seven Tools, the Orchestrator, and its console in WSL:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\test-wsl-orchestrator.ps1
```

The stack uses `/tmp/agent-orchestrator-workspace`, so write tests do not modify the repository. At the `orchestrator>` prompt, enter:

```text
tools discover
route read.lines
call auto read.lines {"path":"README.md","start_line":1,"end_line":5}
call search-text search_text.search {"path":".","query":"TODO","limit":10}
call write-file write_file.create {"path":"demo.txt","content":"hello\n"}
health
```

To add a future Tool, append its `name`, `socket`, and `method_prefixes` to the Tool registry JSON, then enter `reload`. No Orchestrator code changes are required.

Run a repeatable local performance scenario with the load client:

```bash
go run ./cmd/agent-orchestrator-load \
  -socket /tmp/agent-orchestrator/orchestrator.sock \
  -method read.lines \
  -params-file ./configs/load/read-lines.json \
  -requests 1000 -concurrency 8
```

The report includes throughput, error count, mean latency, and P50/P95/P99 latency. Parameter files support `{{id}}` and `{{run}}` placeholders for unique write paths.

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

File Search Tool configuration is independent:

| Environment variable | Default |
| --- | --- |
| `FILE_SEARCH_TOOL_TRANSPORT` | `unix` |
| `FILE_SEARCH_TOOL_SOCKET` | `/run/agent/file-search-tool.sock` |
| `FILE_SEARCH_TOOL_WORKSPACE` | `/workspace` |
| `FILE_SEARCH_TOOL_MAX_FILE_BYTES` | `8388608` |
| `FILE_SEARCH_TOOL_MAX_RESULTS` | `1000` |
| `FILE_SEARCH_TOOL_MAX_SCANNED_ENTRIES` | `200000` |
| `FILE_SEARCH_TOOL_MAX_DEPTH` | `64` |
| `FILE_SEARCH_TOOL_MAX_CONCURRENT` | `8` |

Read File Tool configuration is independent:

| Environment variable | Default |
| --- | --- |
| `READ_FILE_TOOL_TRANSPORT` | `unix` |
| `READ_FILE_TOOL_SOCKET` | `/run/agent/read-file-tool.sock` |
| `READ_FILE_TOOL_WORKSPACE` | `/workspace` |
| `READ_FILE_TOOL_MAX_TEXT_BYTES` | `8388608` |
| `READ_FILE_TOOL_MAX_CHUNK_BYTES` | `1048576` |
| `READ_FILE_TOOL_MAX_HASH_BYTES` | `1073741824` |
| `READ_FILE_TOOL_MAX_CONCURRENT` | `8` |
| `READ_FILE_TOOL_MAX_BATCH_ITEMS` | `20` |

Read Folder Tool configuration is independent:

| Environment variable | Default |
| --- | --- |
| `READ_FOLDER_TOOL_TRANSPORT` | `unix` |
| `READ_FOLDER_TOOL_SOCKET` | `/run/agent/read-folder-tool.sock` |
| `READ_FOLDER_TOOL_WORKSPACE` | `/workspace` |
| `READ_FOLDER_TOOL_MAX_DEPTH` | `32` |
| `READ_FOLDER_TOOL_MAX_SCANNED_ENTRIES` | `100000` |
| `READ_FOLDER_TOOL_MAX_RESULTS` | `5000` |
| `READ_FOLDER_TOOL_DEFAULT_LIMIT` | `100` |
| `READ_FOLDER_TOOL_MAX_HASH_BYTES` | `67108864` |
| `READ_FOLDER_TOOL_MAX_CONCURRENT` | `8` |
| `READ_FOLDER_TOOL_MAX_BATCH_ITEMS` | `20` |

Search Text Tool configuration is independent:

| Environment variable | Default |
| --- | --- |
| `SEARCH_TEXT_TOOL_TRANSPORT` | `unix` |
| `SEARCH_TEXT_TOOL_SOCKET` | `/run/agent/search-text-tool.sock` |
| `SEARCH_TEXT_TOOL_WORKSPACE` | `/workspace` |
| `SEARCH_TEXT_TOOL_MAX_DEPTH` | `32` |
| `SEARCH_TEXT_TOOL_MAX_SCANNED_FILES` | `100000` |
| `SEARCH_TEXT_TOOL_MAX_FILE_BYTES` | `8388608` |
| `SEARCH_TEXT_TOOL_MAX_RESULTS` | `1000` |
| `SEARCH_TEXT_TOOL_DEFAULT_LIMIT` | `100` |
| `SEARCH_TEXT_TOOL_MAX_MATCHES_PER_FILE` | `100` |
| `SEARCH_TEXT_TOOL_MAX_CONTEXT_LINES` | `20` |
| `SEARCH_TEXT_TOOL_MAX_CONCURRENT` | `8` |
| `SEARCH_TEXT_TOOL_MAX_BATCH_ITEMS` | `20` |

Write File Tool configuration is independent:

| Environment variable | Default |
| --- | --- |
| `WRITE_FILE_TOOL_TRANSPORT` | `unix` |
| `WRITE_FILE_TOOL_SOCKET` | `/run/agent/write-file-tool.sock` |
| `WRITE_FILE_TOOL_WORKSPACE` | `/workspace` |
| `WRITE_FILE_TOOL_TEMP_DIR` | `/tmp/agent-write-file` |
| `WRITE_FILE_TOOL_MAX_MESSAGE_BYTES` | `16777216` |
| `WRITE_FILE_TOOL_MAX_FILE_BYTES` | `8388608` |
| `WRITE_FILE_TOOL_MAX_BATCH_FILES` | `100` |
| `WRITE_FILE_TOOL_MAX_BATCH_BYTES` | `67108864` |
| `WRITE_FILE_TOOL_MAX_ROLLBACK_BYTES` | `268435456` |
| `WRITE_FILE_TOOL_MAX_CONCURRENT` | `8` |
| `WRITE_FILE_TOOL_JOURNAL_TTL` | `15m` |

Agent Orchestrator configuration:

| Environment variable | Default |
| --- | --- |
| `AGENT_ORCHESTRATOR_TRANSPORT` | `unix` |
| `AGENT_ORCHESTRATOR_SOCKET` | `/run/agent/orchestrator.sock` |
| `AGENT_ORCHESTRATOR_TOOLS_FILE` | `/etc/agent/orchestrator-tools.json` |
| `AGENT_ORCHESTRATOR_MAX_MESSAGE_BYTES` | `16777216` |
| `AGENT_ORCHESTRATOR_MAX_CONCURRENT` | `32` |
| `AGENT_ORCHESTRATOR_DEFAULT_TIMEOUT` | `2m` |
| `AGENT_ORCHESTRATOR_SHUTDOWN_GRACE` | `5s` |

Docker is the primary security boundary. The service intentionally does not parse command semantics or maintain a command blacklist; callers can replace the default allow-all `ExecutionPolicy` when embedding the server.
