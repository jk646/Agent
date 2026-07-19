package filesearch

type Config struct {
	Workspace         string
	MaxFileBytes      int64
	MaxResults        int
	MaxScannedEntries int
	MaxDepth          int
	MaxConcurrent     int
	IgnoredNames      []string
}

type StatParams struct {
	Path string `json:"path"`
}

type Entry struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at"`
	Score      int    `json:"score,omitempty"`
}

type FindParams struct {
	SearchID       string   `json:"search_id,omitempty"`
	Path           string   `json:"path,omitempty"`
	Pattern        string   `json:"pattern,omitempty"`
	Name           string   `json:"name,omitempty"`
	Type           string   `json:"type,omitempty"`
	Extensions     []string `json:"extensions,omitempty"`
	IncludeHidden  bool     `json:"include_hidden,omitempty"`
	MaxDepth       int      `json:"max_depth,omitempty"`
	MinSize        int64    `json:"min_size,omitempty"`
	MaxSize        int64    `json:"max_size,omitempty"`
	ModifiedAfter  string   `json:"modified_after,omitempty"`
	ModifiedBefore string   `json:"modified_before,omitempty"`
	Cursor         int      `json:"cursor,omitempty"`
	Limit          int      `json:"limit,omitempty"`
}

type FindResult struct {
	SearchID       string  `json:"search_id"`
	Matches        []Entry `json:"matches"`
	NextCursor     int     `json:"next_cursor,omitempty"`
	Truncated      bool    `json:"truncated"`
	ScannedEntries int     `json:"scanned_entries"`
	DurationMS     int64   `json:"duration_ms"`
}

type ContentParams struct {
	SearchID      string `json:"search_id,omitempty"`
	Path          string `json:"path,omitempty"`
	Query         string `json:"query"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	FilePattern   string `json:"file_pattern,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	MaxDepth      int    `json:"max_depth,omitempty"`
	ContextBefore int    `json:"context_before,omitempty"`
	ContextAfter  int    `json:"context_after,omitempty"`
	Cursor        int    `json:"cursor,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type ContentMatch struct {
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Column int      `json:"column"`
	Text   string   `json:"text"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

type ContentResult struct {
	SearchID      string         `json:"search_id"`
	Matches       []ContentMatch `json:"matches"`
	NextCursor    int            `json:"next_cursor,omitempty"`
	Truncated     bool           `json:"truncated"`
	ScannedFiles  int            `json:"scanned_files"`
	SkippedBinary int            `json:"skipped_binary"`
	SkippedLarge  int            `json:"skipped_large"`
	DurationMS    int64          `json:"duration_ms"`
}

type SearchRequest struct {
	Kind    string         `json:"kind"`
	Find    *FindParams    `json:"find,omitempty"`
	Content *ContentParams `json:"content,omitempty"`
}

type BatchParams struct {
	Searches []SearchRequest `json:"searches"`
}

type BatchItem struct {
	Kind    string         `json:"kind"`
	Find    *FindResult    `json:"find,omitempty"`
	Content *ContentResult `json:"content,omitempty"`
}

type BatchResult struct {
	Results []BatchItem `json:"results"`
}

type CancelParams struct {
	SearchID string `json:"search_id"`
}

type CancelResult struct {
	SearchID string `json:"search_id"`
	Canceled bool   `json:"canceled"`
}
