# JSON-RPC Protocol v1

Transport framing is UTF-8 JSON Lines. Requests use JSON-RPC 2.0 and events are JSON-RPC notifications.

## Initialize

```json
{"jsonrpc":"2.0","id":1,"method":"system.initialize","params":{"protocol_version":"1","client_info":{"name":"orchestrator"}}}
```

## Stateless execution

```json
{"jsonrpc":"2.0","id":2,"method":"exec.start","params":{"request_id":"req-123","command":"go test ./...","cwd":"/workspace","timeout_ms":120000,"output_limit_bytes":1048576}}
```

The immediate response acknowledges acceptance. Output arrives independently:

```json
{"jsonrpc":"2.0","method":"exec.started","params":{"request_id":"req-123","pid":42,"timestamp":"..."}}
{"jsonrpc":"2.0","method":"exec.output","params":{"request_id":"req-123","sequence":1,"stream":"stdout","data_base64":"b2sK","timestamp":"..."}}
{"jsonrpc":"2.0","method":"exec.exited","params":{"request_id":"req-123","exit_code":0,"duration_ms":15,"timed_out":false,"canceled":false,"total_output_bytes":3,"truncated":false,"timestamp":"..."}}
```

Use `exec.write` for enabled stdin and `exec.cancel` to terminate the process group.

## Persistent sessions

1. `session.open` creates a Bash PTY.
2. `session.run` evaluates one command in the existing shell and returns its exit status.
3. `session.write` sends raw base64 input to the terminal.
4. `session.resize` updates terminal rows and columns.
5. `session.interrupt` sends `SIGINT` to the foreground process group.
6. `session.close` terminates the session.
7. `session.list` returns current session metadata.

```json
{"jsonrpc":"2.0","id":3,"method":"session.open","params":{"session_id":"dev","cwd":"/workspace","rows":24,"cols":120}}
{"jsonrpc":"2.0","id":4,"method":"session.run","params":{"session_id":"dev","run_id":"run-1","command":"cd src && export MODE=test"}}
```

PTY output uses `session.output`. Because a PTY is a terminal byte stream, stdout and stderr are intentionally merged.

## Errors

| Code | Meaning |
| --- | --- |
| `-32601` | Method not found |
| `-32602` | Invalid parameters |
| `-32001` | Identifier conflict |
| `-32002` | Execution or session not found |
| `-32003` | Session busy |
| `-32004` | Capacity reached |
| `-32005` | Execution policy rejected |