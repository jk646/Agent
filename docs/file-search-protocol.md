# Agent File Search Tool Protocol

The File Search Tool is an independent read-only Linux process. It uses JSON-RPC 2.0 with JSONL over `/run/agent/file-search-tool.sock` or stdio. It does not import or call the Shell Tool or File Edit Tool.

## Methods

- `system.initialize`
- `system.capabilities`
- `system.health`
- `system.shutdown`
- `search.stat`
- `search.find`
- `search.content`
- `search.batch`
- `search.cancel`

## Find files

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "search.find",
  "params": {
    "search_id": "optional-client-id",
    "path": ".",
    "pattern": "*.go",
    "name": "server",
    "type": "file",
    "extensions": [".go"],
    "max_depth": 10,
    "min_size": 0,
    "max_size": 1048576,
    "include_hidden": false,
    "cursor": 0,
    "limit": 100
  }
}
```

`pattern` uses Go-style glob matching against both the basename and workspace-relative path. `name` is a case-insensitive substring search. Exact and prefix matches receive a higher score.

Metadata filters include `type`, `extensions`, `min_size`, `max_size`, `modified_after`, and `modified_before`. Times use RFC3339.

## Search content

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "search.content",
  "params": {
    "path": ".",
    "query": "TODO",
    "regex": false,
    "case_sensitive": false,
    "file_pattern": "*.go",
    "max_depth": 10,
    "context_before": 1,
    "context_after": 2,
    "cursor": 0,
    "limit": 100
  }
}
```

Matches include path, line, Unicode-aware column, text, and optional surrounding lines. Binary files, symbolic links, ignored directories, and files larger than the configured limit are skipped.

## Batch and cancellation

`search.batch` executes up to 20 independent searches sequentially:

```json
{
  "searches": [
    {"kind": "find", "find": {"pattern": "*.md"}},
    {"kind": "content", "content": {"query": "JSON-RPC"}}
  ]
}
```

To cancel a long-running request, set a known `search_id` in the original request and send from another connection:

```json
{"jsonrpc":"2.0","id":3,"method":"search.cancel","params":{"search_id":"manual-1"}}
```

## Safety and limits

- All paths are relative to the configured workspace.
- Absolute paths, NUL bytes, `..` escapes, and symlink traversal are rejected.
- Default ignored names are `.git`, `node_modules`, `vendor`, `.idea`, `.vscode`, `dist`, and `build`.
- Result count, scanned entries, depth, file size, request size, and concurrent searches are bounded.
- Disconnecting a client cancels searches started by that connection.
- v1 uses direct filesystem scanning and keeps no persistent index.

## Error codes

| Code | Meaning |
| --- | --- |
| `-32200` | Path outside workspace |
| `-32201` | Symlink rejected |
| `-32202` | Invalid search request |
| `-32203` | Search limit exceeded |
| `-32204` | Concurrent search capacity reached |
| `-32205` | Search ID already active |
| `-32206` | Active search ID not found |
| `-32207` | Search canceled |
