# Agent Read Folder Tool Protocol

The Read Folder Tool is an independent read-only Linux process. It uses JSON-RPC 2.0 with JSONL over `/run/agent/read-folder-tool.sock` or stdio. It never invokes or imports another Tool's business packages.

## Methods

- `system.initialize`
- `system.capabilities`
- `system.health`
- `system.shutdown`
- `read_folder.stat`
- `read_folder.list`
- `read_folder.tree`
- `read_folder.summary`
- `read_folder.snapshot`
- `read_folder.compare`
- `read_folder.batch`
- `read_folder.cancel`

## Folder metadata

```json
{"jsonrpc":"2.0","id":1,"method":"read_folder.stat","params":{"path":"internal","include_digest":true}}
```

The response contains direct child file, folder, and other counts, the folder mode and modification time, empty state, and an optional deterministic digest of direct child metadata.

## List folder entries

```json
{
  "path": "internal",
  "depth": 3,
  "include_hidden": false,
  "include_files": true,
  "include_folders": true,
  "name_pattern": "*.go",
  "extensions": [".go"],
  "sort_by": "size",
  "sort_order": "desc",
  "cursor": 0,
  "limit": 100
}
```

Filtering supports name glob, extensions, size range, RFC3339 modification range, files, folders, and hidden entries. Sorting supports `name`, `path`, `type`, `size`, and `modified_at` in ascending or descending order.

## Build a folder tree

```json
{
  "path": ".",
  "depth": 3,
  "include_files": true,
  "include_hidden": false,
  "max_entries": 500
}
```

The result contains a nested root node, entry count, scanned count, skipped symlink count, and truncation state.

## Summarize a folder

```json
{"path":".","depth":20,"include_hidden":false}
```

The summary reports file, folder, and other counts, total regular-file bytes, maximum depth, empty folders, extension counts, largest file, most recently modified file, skipped symlinks, and truncation.

## Snapshot and compare

```json
{
  "path": ".",
  "depth": 10,
  "include_file_hashes": true,
  "limit": 5000
}
```

A snapshot returns a random `snapshot_id`, deterministic aggregate digest, and sorted metadata entries. File hashes are optional and files larger than the hash limit are marked `hash_skipped`.

The Tool does not persist snapshots. Pass a previous snapshot's entries back to compare with current state:

```json
{
  "path": ".",
  "depth": 10,
  "include_file_hashes": true,
  "previous_entries": []
}
```

The result contains sorted `added`, `removed`, and `modified` paths plus an unchanged count. Comparison requires a complete current traversal and fails instead of returning an incomplete change set.

## Batch and cancellation

```json
{
  "reads": [
    {"kind":"stat","stat":{"path":"cmd"}},
    {"kind":"summary","summary":{"path":"internal","depth":3}}
  ]
}
```

Long-running calls can set a known `read_id`. Cancel from another connection:

```json
{"jsonrpc":"2.0","id":9,"method":"read_folder.cancel","params":{"read_id":"folder-read-manual"}}
```

## Safety behavior

- Paths must be relative to the configured workspace.
- Absolute paths, NUL bytes, `..` escapes, and symlink traversal are rejected.
- Traversed symlinks are skipped and counted, never followed.
- The Tool reads folder structure and metadata but never file content except optional bounded hashing.
- Depth, scanned entries, returned entries, hash size, request size, batch size, and concurrency are bounded.
- Client disconnect and service shutdown cancel active traversals.
- Optional ignored names are configured with `READ_FOLDER_TOOL_IGNORED_NAMES`.

## Error codes

| Code | Meaning |
| --- | --- |
| `-32400` | Path outside workspace |
| `-32401` | Symlink rejected |
| `-32402` | Invalid folder read request |
| `-32403` | Folder read limit exceeded |
| `-32404` | Concurrent folder read capacity reached |
| `-32405` | Read ID already active |
| `-32406` | Active read ID not found |
| `-32407` | Folder read canceled |
| `-32408` | Path is not a folder |
