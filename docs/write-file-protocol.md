# Write File Tool Protocol

The Write File Tool is an independent JSON-RPC 2.0 service for creating and writing complete files. Each JSON message occupies one line. The default socket is `/run/agent/write-file-tool.sock`, and protocol version is `1`.

It never invokes Shell, File Edit, File Search, Read File, Read Folder, Search Text, or an external command. File Edit remains responsible for replacements and patches; Write File handles complete content and byte-oriented writes.

## Methods

| Method | Purpose |
| --- | --- |
| `write_file.create` | Create a new file and reject an existing target |
| `write_file.overwrite` | Atomically replace complete file content |
| `write_file.append` | Append text or bytes through an atomic replacement |
| `write_file.write_at` | Write bytes at an offset |
| `write_file.preview` | Validate and return changes without writing |
| `write_file.batch` | Prepare and commit multiple files as one transaction |
| `write_file.rollback` | Restore a committed transaction when files are unchanged |
| `write_file.cancel` | Cancel an active write before commit completes |
| `system.initialize` | Negotiate protocol version and capabilities |
| `system.capabilities` | Return methods, workspace, and encodings |
| `system.health` | Return health and active write count |
| `system.shutdown` | Cancel active writes and stop the service |

## Operations

Create a UTF-8 text file:

```json
{
  "path": "config/app.json",
  "content": "{\"enabled\":true}\n",
  "create_parents": true,
  "mode": "0644"
}
```

Overwrite with optimistic concurrency protection:

```json
{
  "path": "config/app.json",
  "content": "{\"enabled\":false}\n",
  "expected_sha256": "64-hex-character-digest",
  "create_if_missing": false
}
```

Append text:

```json
{
  "path": "logs/events.txt",
  "content": "completed",
  "add_newline": true,
  "create_if_missing": true,
  "create_parents": true
}
```

Write binary bytes at an offset:

```json
{
  "path": "data/index.bin",
  "offset": 128,
  "data_base64": "AQIDBA==",
  "expected_sha256": "64-hex-character-digest",
  "allow_sparse": false
}
```

`content` and `data_base64` are mutually exclusive. An omitted payload writes zero bytes. Modes must be octal and cannot exceed `0666`. Absolute paths, NUL bytes, workspace escapes, symlinks, directories, devices, sockets, and FIFOs are rejected.

## Batch and preview

```json
{
  "write_id": "optional-client-id",
  "operations": [
    {"kind":"create","path":"a.txt","content":"a\n"},
    {"kind":"overwrite","path":"b.txt","content":"b\n","expected_sha256":"..."}
  ]
}
```

Supported operation kinds are `create`, `overwrite`, `append`, and `write_at`. Duplicate normalized paths in one batch are rejected. Paths are locked in lexical order. All operations are validated and prepared before commit. If a later commit fails, already committed files are restored from the journal.

Send the same shape to `write_file.preview`; it returns SHA-256 changes and a bounded whole-file text diff without changing disk.

## Result and rollback

```json
{
  "write_id": "write-abc",
  "transaction_id": "writetx-xyz",
  "applied": true,
  "preview": false,
  "rollback_available": true,
  "files": [{
    "path": "a.txt",
    "action": "created",
    "size": 2,
    "after_sha256": "..."
  }]
}
```

Rollback request:

```json
{"transaction_id":"writetx-xyz"}
```

Rollback verifies every current file against the transaction's `after_sha256`. If any file changed after the transaction, the complete rollback is rejected.

## Atomicity

Each result is written to a temporary file in the target directory, flushed with `fsync`, and committed with same-filesystem `rename`. The parent directory is then flushed. Docker remains the primary isolation boundary.

## Error codes

| Code | Meaning |
| --- | --- |
| `-32520` | Path outside workspace |
| `-32521` | Symlink rejected |
| `-32522` | Invalid operation or parameters |
| `-32523` | File, batch, or rollback limit exceeded |
| `-32524` | SHA-256 concurrency check failed |
| `-32525` | Create target already exists |
| `-32526` | Target is not a regular file |
| `-32527` | Concurrent write capacity reached |
| `-32528` | Duplicate active write ID |
| `-32529` | Active write ID not found |
| `-32530` | Rollback conflict |
| `-32531` | Transaction not found |
| `-32532` | Write canceled |
