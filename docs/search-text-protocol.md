# Search Text Tool Protocol

The Search Text Tool is an independent, read-only JSON-RPC 2.0 service. Each JSON message occupies one line. The default Unix socket is `/run/agent/search-text-tool.sock`, and protocol version is `1`.

It searches in-process with Go's RE2 regular expression engine. It never invokes the Shell Tool, File Tool, File Search Tool, Read File Tool, Read Folder Tool, or system `grep`.

## Methods

| Method | Purpose |
| --- | --- |
| `search_text.search` | Literal text search |
| `search_text.regex` | RE2 regular expression search |
| `search_text.multi` | Search multiple literal or regex patterns |
| `search_text.files` | Return matching file paths only |
| `search_text.count` | Return match counts grouped by file |
| `search_text.batch` | Run 1 to the configured maximum searches serially |
| `search_text.cancel` | Cancel an active search by `search_id` |
| `system.initialize` | Negotiate protocol version and capabilities |
| `system.capabilities` | Return supported methods and encodings |
| `system.health` | Return health and active search count |
| `system.shutdown` | Stop accepting work and cancel active searches |

## Search parameters

```json
{
  "search_id": "optional-client-id",
  "path": ".",
  "query": "TODO",
  "case_sensitive": false,
  "whole_word": true,
  "invert_match": false,
  "include_patterns": ["**/*.go"],
  "exclude_patterns": ["vendor/**"],
  "extensions": [".go"],
  "include_hidden": false,
  "max_depth": 16,
  "max_file_bytes": 1048576,
  "context_before": 1,
  "context_after": 1,
  "max_matches_per_file": 100,
  "cursor": 0,
  "limit": 100
}
```

`file_pattern` is a backward-compatible single include pattern. `*` does not cross `/`; `**` does. Requested bounds are clamped to server limits. Hidden paths, configured ignored names, symlinks, non-regular files, oversized files, and binary files are skipped.

`search_text.regex` treats `query` as RE2. `search_text.search` always treats it literally. Empty-matching regular expressions are rejected. `whole_word` treats Unicode letters, Unicode numbers, and `_` as word characters.

## Match result

```json
{
  "search_id": "text-search-abc",
  "matches": [{
    "pattern_id": "todo",
    "path": "internal/server/server.go",
    "line": 42,
    "column": 5,
    "byte_offset": 1024,
    "text": "// TODO: implement",
    "match": "TODO",
    "before": ["// context"],
    "after": ["func next() {}"]
  }],
  "next_cursor": 100,
  "truncated": true,
  "scanned_files": 250,
  "skipped_binary": 2,
  "skipped_large": 1,
  "duration_ms": 12
}
```

Line and column are 1-based. Column counts Unicode code points. `byte_offset` refers to the original file bytes, including any BOM and UTF-16 code-unit width. Supported text encodings are UTF-8, UTF-8 with BOM, UTF-16LE with BOM, and UTF-16BE with BOM. PTY or shell semantics are not involved.

## Multi-pattern search

```json
{
  "path": ".",
  "patterns": [
    {"id": "todo", "query": "TODO", "whole_word": true},
    {"id": "fixme", "query": "FIXME|XXX", "regex": true, "case_sensitive": true}
  ],
  "extensions": ["go"]
}
```

Each match includes its `pattern_id`. `invert_match` is intentionally rejected for multi-pattern requests because its meaning is ambiguous.

## Batch and cancellation

Batch items use `kind` values `search`, `regex`, `multi`, `files`, or `count`, with parameters under `search` or `multi`.

```json
{"searches":[{"kind":"search","search":{"path":".","query":"TODO"}},{"kind":"count","search":{"path":".","query":"error"}}]}
```

Cancel with:

```json
{"search_id":"text-search-abc"}
```

## Error codes

| Code | Meaning |
| --- | --- |
| `-32500` | Path is outside workspace |
| `-32501` | Symlink is rejected |
| `-32502` | Invalid request or pattern |
| `-32503` | Configured limit exceeded |
| `-32504` | Binary or unsupported encoding |
| `-32505` | Concurrent search capacity reached |
| `-32506` | Duplicate active search ID |
| `-32507` | Active search ID not found |
| `-32508` | Search canceled |
