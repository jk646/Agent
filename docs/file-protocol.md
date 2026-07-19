# Agent File Tool Protocol

The File Tool is an independent Linux process. It uses JSON-RPC 2.0 with one JSON message per line over `/run/agent/file-tool.sock` or stdio. Paths are always relative to the configured workspace.

## Service methods

- `system.initialize`: negotiate `protocol_version: "1"`.
- `system.capabilities`: list supported methods and the workspace root.
- `system.health`: return status, uptime, workspace, and active transactions.
- `system.shutdown`: stop accepting work and close the service.

## Query methods

- `file.stat`: `{"path":"demo.txt","include_hash":true}`
- `file.read`: `{"path":"demo.txt","start_line":1,"end_line":100,"max_bytes":262144}`
- `file.list`: `{"path":"src","depth":2,"include_hidden":false,"include_hash":false}`
- `file.find`: `{"path":".","pattern":"*.go","type":"file","limit":100}`
- `file.search`: `{"path":".","query":"TODO","regex":false,"case_sensitive":true,"limit":100}`

`file.read` and `file.stat` return SHA-256 values. Pass that hash when mutating an existing path to prevent overwriting changes made by another tool or process.

## Mutation methods

Single-operation methods accept an operation object:

- `file.create`: `{"path":"demo.txt","content":"hello\n","create_parents":true}`
- `file.mkdir`: `{"path":"tmp/example","create_parents":true,"mode":"0755"}`
- `file.copy`: `{"from":"demo.txt","to":"copy.txt","expected_sha256":"..."}`
- `file.move`: `{"from":"copy.txt","to":"archive/copy.txt","expected_sha256":"...","create_parents":true}`
- `file.delete`: `{"path":"archive/copy.txt","expected_sha256":"...","recursive":false}`
- `file.chmod`: `{"path":"demo.txt","expected_sha256":"...","mode":"0644"}`

Use `file.apply_edits` for exact text replacement:

```json
{
  "changes": [{
    "kind": "replace",
    "path": "demo.txt",
    "expected_sha256": "...",
    "replacements": [{
      "old_text": "hello",
      "new_text": "updated",
      "expected_occurrences": 1
    }]
  }],
  "dry_run": false
}
```

Use `file.batch` to apply multiple operations atomically. Set `dry_run: true` to return a diff without changing the workspace. Successful non-permanent mutations return a `transaction_id`; restore the exact pre-change state with:

```json
{"jsonrpc":"2.0","id":9,"method":"file.rollback","params":{"transaction_id":"filetx-..."}}
```

Rollback is rejected if any affected path changed after the transaction.

## Safety behavior

- Absolute paths, `..` escapes, NUL bytes, and workspace escapes are rejected.
- Symlink traversal is rejected by default.
- Existing sources and mutation targets require SHA-256 preconditions.
- Writes use a temporary file, `fsync`, and atomic rename.
- Mutations take sorted per-path locks and retain a bounded rollback journal.
- The File Tool never invokes a shell or imports Shell Tool execution packages.

## Error codes

| Code | Meaning |
| --- | --- |
| `-32100` | Path outside workspace |
| `-32101` | SHA-256 precondition failed |
| `-32102` | Replacement text not found |
| `-32103` | Replacement count mismatch |
| `-32104` | File, request, or transaction too large |
| `-32105` | Binary content rejected |
| `-32106` | Destination already exists |
| `-32107` | Symlink rejected |
| `-32108` | Rollback conflict |
| `-32109` | Invalid file operation |
