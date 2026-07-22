# Agent Read File Tool Protocol

The Read File Tool is an independent read-only Linux process. It uses JSON-RPC 2.0 with JSONL over `/run/agent/read-file-tool.sock` or stdio. It does not import or call the Shell, File Edit, or File Search Tool.

## Methods

- `system.initialize`
- `system.capabilities`
- `system.health`
- `system.shutdown`
- `read.stat`
- `read.text`
- `read.lines`
- `read.bytes`
- `read.binary`
- `read.hash`
- `read.batch`
- `read.cancel`

## File metadata

```json
{"jsonrpc":"2.0","id":1,"method":"read.stat","params":{"path":"README.md","include_hash":true}}
```

The response includes the workspace-relative path, type, size, Unix mode, modification time, optional SHA-256, and detected encoding.

## Read lines

Line numbers are one-based and inclusive:

```json
{
  "path": "internal/server/server.go",
  "start_line": 20,
  "end_line": 80,
  "max_bytes": 262144,
  "include_line_numbers": true
}
```

The result includes `total_lines`, `next_line`, `truncated`, encoding, newline style, and SHA-256. Pagination never splits a line. A single line larger than `max_bytes` returns a size error.

## Read text

Character offsets are zero-based and `end_char` is exclusive:

```json
{"path":"README.md","start_char":0,"end_char":500,"max_bytes":4096}
```

Character slicing is Unicode-aware. The response returns `next_char` when the output byte limit truncates the requested range.

Supported text encodings are UTF-8, UTF-8 with BOM, UTF-16LE with BOM, and UTF-16BE with BOM. Other data must be read with the binary interface.

## Read bytes or binary

```json
{
  "path": "assets/image.png",
  "offset": 0,
  "length": 65536,
  "include_hash": true
}
```

`read.bytes` and `read.binary` are aliases. Data is returned as `data_base64` with `bytes_read`, `next_offset`, and `eof`. Offsets are zero-based bytes.

## Hash, batch, and cancellation

```json
{"jsonrpc":"2.0","id":4,"method":"read.hash","params":{"read_id":"manual-hash","path":"large.bin"}}
```

`read.batch` executes up to the configured maximum number of read operations sequentially:

```json
{
  "reads": [
    {"kind":"stat","stat":{"path":"README.md"}},
    {"kind":"lines","lines":{"path":"README.md","start_line":1,"end_line":10}}
  ]
}
```

To cancel an active operation, provide a known `read_id` and send from another connection:

```json
{"jsonrpc":"2.0","id":5,"method":"read.cancel","params":{"read_id":"manual-hash"}}
```

## Safety behavior

- Paths must remain inside the configured workspace.
- Absolute paths, NUL bytes, `..` escapes, and symlink traversal are rejected.
- Only regular files can be read or hashed.
- Text size, binary chunk size, hash size, request size, batch size, and concurrency are bounded.
- The service compares file metadata before and after reads and rejects unstable results.
- Disconnecting a client cancels reads started by that connection.

## Error codes

| Code | Meaning |
| --- | --- |
| `-32300` | Path outside workspace |
| `-32301` | Symlink rejected |
| `-32302` | Invalid read request |
| `-32303` | Read limit exceeded |
| `-32304` | Unsupported or binary text encoding |
| `-32305` | Concurrent read capacity reached |
| `-32306` | Read ID already active |
| `-32307` | Active read ID not found |
| `-32308` | Read canceled |
| `-32309` | File changed during read |
| `-32310` | Path is not a regular file |
