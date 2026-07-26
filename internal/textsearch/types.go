package textsearch

type Config struct {
	Workspace       string
	MaxDepth        int
	MaxScannedFiles int
	MaxFileBytes    int64
	MaxResults      int
	DefaultLimit    int
	MaxMatchesFile  int
	MaxContextLines int
	MaxConcurrent   int
	MaxBatchItems   int
	IgnoredNames    []string
}

type SearchParams struct {
	SearchID          string   `json:"search_id,omitempty"`
	Path              string   `json:"path,omitempty"`
	Query             string   `json:"query"`
	Regex             bool     `json:"regex,omitempty"`
	CaseSensitive     bool     `json:"case_sensitive,omitempty"`
	WholeWord         bool     `json:"whole_word,omitempty"`
	FixedString       bool     `json:"fixed_string,omitempty"`
	InvertMatch       bool     `json:"invert_match,omitempty"`
	FilePattern       string   `json:"file_pattern,omitempty"`
	IncludePatterns   []string `json:"include_patterns,omitempty"`
	ExcludePatterns   []string `json:"exclude_patterns,omitempty"`
	Extensions        []string `json:"extensions,omitempty"`
	IncludeHidden     bool     `json:"include_hidden,omitempty"`
	MaxDepth          int      `json:"max_depth,omitempty"`
	MaxFileBytes      int64    `json:"max_file_bytes,omitempty"`
	ContextBefore     int      `json:"context_before,omitempty"`
	ContextAfter      int      `json:"context_after,omitempty"`
	MaxMatchesPerFile int      `json:"max_matches_per_file,omitempty"`
	Cursor            int      `json:"cursor,omitempty"`
	Limit             int      `json:"limit,omitempty"`
}

type Pattern struct {
	ID            string `json:"id,omitempty"`
	Query         string `json:"query"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	WholeWord     bool   `json:"whole_word,omitempty"`
}

type MultiParams struct {
	SearchParams
	Patterns []Pattern `json:"patterns"`
}

type Match struct {
	PatternID  string   `json:"pattern_id,omitempty"`
	Path       string   `json:"path"`
	Line       int      `json:"line"`
	Column     int      `json:"column"`
	ByteOffset int64    `json:"byte_offset"`
	Text       string   `json:"text"`
	Match      string   `json:"match"`
	Before     []string `json:"before,omitempty"`
	After      []string `json:"after,omitempty"`
}

type SearchResult struct {
	SearchID      string  `json:"search_id"`
	Matches       []Match `json:"matches"`
	NextCursor    int     `json:"next_cursor,omitempty"`
	Truncated     bool    `json:"truncated"`
	ScannedFiles  int     `json:"scanned_files"`
	SkippedBinary int     `json:"skipped_binary"`
	SkippedLarge  int     `json:"skipped_large"`
	DurationMS    int64   `json:"duration_ms"`
}

type FilesResult struct {
	SearchID      string   `json:"search_id"`
	Paths         []string `json:"paths"`
	NextCursor    int      `json:"next_cursor,omitempty"`
	Truncated     bool     `json:"truncated"`
	ScannedFiles  int      `json:"scanned_files"`
	SkippedBinary int      `json:"skipped_binary"`
	SkippedLarge  int      `json:"skipped_large"`
	DurationMS    int64    `json:"duration_ms"`
}

type FileCount struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}
type CountResult struct {
	SearchID      string      `json:"search_id"`
	Files         []FileCount `json:"files"`
	Total         int         `json:"total"`
	NextCursor    int         `json:"next_cursor,omitempty"`
	Truncated     bool        `json:"truncated"`
	ScannedFiles  int         `json:"scanned_files"`
	SkippedBinary int         `json:"skipped_binary"`
	SkippedLarge  int         `json:"skipped_large"`
	DurationMS    int64       `json:"duration_ms"`
}

type SearchRequest struct {
	Kind   string        `json:"kind"`
	Search *SearchParams `json:"search,omitempty"`
	Multi  *MultiParams  `json:"multi,omitempty"`
}
type BatchParams struct {
	Searches []SearchRequest `json:"searches"`
}
type BatchItem struct {
	Kind   string        `json:"kind"`
	Search *SearchResult `json:"search,omitempty"`
	Files  *FilesResult  `json:"files,omitempty"`
	Count  *CountResult  `json:"count,omitempty"`
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
