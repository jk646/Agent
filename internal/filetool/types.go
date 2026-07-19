package filetool

import "time"

type Config struct {
	Workspace           string
	TempDir             string
	MaxFileBytes        int64
	MaxReadBytes        int
	MaxEntries          int
	MaxSearchMatches    int
	MaxTransactionFiles int
	MaxTransactionBytes int64
	MaxDiffBytes        int
	MaxConcurrent       int
	JournalTTL          time.Duration
}

type StatParams struct {
	Path        string `json:"path"`
	IncludeHash bool   `json:"include_hash,omitempty"`
}

type FileInfo struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at"`
	SHA256     string `json:"sha256,omitempty"`
}

type ReadParams struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type ReadResult struct {
	Path       string `json:"path"`
	Content    string `json:"content"`
	SHA256     string `json:"sha256"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	TotalLines int    `json:"total_lines"`
	NextLine   int    `json:"next_line,omitempty"`
	Truncated  bool   `json:"truncated"`
	Newline    string `json:"newline"`
}

type ListParams struct {
	Path          string `json:"path,omitempty"`
	Depth         int    `json:"depth,omitempty"`
	Cursor        int    `json:"cursor,omitempty"`
	Limit         int    `json:"limit,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	IncludeHash   bool   `json:"include_hash,omitempty"`
}

type ListResult struct {
	Path       string     `json:"path"`
	Entries    []FileInfo `json:"entries"`
	NextCursor int        `json:"next_cursor,omitempty"`
	Truncated  bool       `json:"truncated"`
}

type FindParams struct {
	Path          string `json:"path,omitempty"`
	Pattern       string `json:"pattern"`
	Type          string `json:"type,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type SearchParams struct {
	Path          string `json:"path,omitempty"`
	Query         string `json:"query"`
	Regex         bool   `json:"regex,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type SearchMatch struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	Text   string `json:"text"`
}

type SearchResult struct {
	Matches   []SearchMatch `json:"matches"`
	Truncated bool          `json:"truncated"`
}

type Replacement struct {
	OldText             string `json:"old_text"`
	NewText             string `json:"new_text"`
	ExpectedOccurrences int    `json:"expected_occurrences,omitempty"`
}

type Operation struct {
	Kind                 string        `json:"kind"`
	Path                 string        `json:"path,omitempty"`
	From                 string        `json:"from,omitempty"`
	To                   string        `json:"to,omitempty"`
	ExpectedSHA256       string        `json:"expected_sha256,omitempty"`
	ExpectedTargetSHA256 string        `json:"expected_target_sha256,omitempty"`
	Content              string        `json:"content,omitempty"`
	Replacements         []Replacement `json:"replacements,omitempty"`
	CreateParents        bool          `json:"create_parents,omitempty"`
	Overwrite            bool          `json:"overwrite,omitempty"`
	Recursive            bool          `json:"recursive,omitempty"`
	Mode                 string        `json:"mode,omitempty"`
}

type BatchParams struct {
	DryRun     bool        `json:"dry_run,omitempty"`
	Permanent  bool        `json:"permanent,omitempty"`
	Operations []Operation `json:"operations"`
}

type ApplyEditsParams struct {
	DryRun    bool        `json:"dry_run,omitempty"`
	Permanent bool        `json:"permanent,omitempty"`
	Changes   []Operation `json:"changes"`
}

type FileChange struct {
	Path         string `json:"path"`
	Action       string `json:"action"`
	BeforeSHA256 string `json:"before_sha256,omitempty"`
	AfterSHA256  string `json:"after_sha256,omitempty"`
}

type TransactionResult struct {
	TransactionID     string       `json:"transaction_id,omitempty"`
	Applied           bool         `json:"applied"`
	RollbackAvailable bool         `json:"rollback_available"`
	Files             []FileChange `json:"files"`
	Diff              string       `json:"diff,omitempty"`
	DiffTruncated     bool         `json:"diff_truncated"`
}

type RollbackParams struct {
	TransactionID string `json:"transaction_id"`
}

type RollbackResult struct {
	TransactionID string       `json:"transaction_id"`
	RolledBack    bool         `json:"rolled_back"`
	Files         []FileChange `json:"files"`
}
