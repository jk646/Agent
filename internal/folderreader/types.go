package folderreader

type Config struct {
	Workspace         string
	MaxDepth          int
	MaxScannedEntries int
	MaxResults        int
	DefaultLimit      int
	MaxHashBytes      int64
	MaxConcurrent     int
	MaxBatchItems     int
	IgnoredNames      []string
}

type Entry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	Mode        string `json:"mode"`
	ModifiedAt  string `json:"modified_at"`
	Depth       int    `json:"depth"`
	Extension   string `json:"extension,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	HashSkipped bool   `json:"hash_skipped,omitempty"`
}

type StatParams struct {
	Path          string `json:"path"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	IncludeDigest bool   `json:"include_digest,omitempty"`
}

type FolderStat struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Mode        string `json:"mode"`
	ModifiedAt  string `json:"modified_at"`
	FileCount   int    `json:"file_count"`
	FolderCount int    `json:"folder_count"`
	OtherCount  int    `json:"other_count"`
	Empty       bool   `json:"empty"`
	Digest      string `json:"digest,omitempty"`
}

type ListParams struct {
	ReadID         string   `json:"read_id,omitempty"`
	Path           string   `json:"path,omitempty"`
	Depth          int      `json:"depth,omitempty"`
	IncludeHidden  bool     `json:"include_hidden,omitempty"`
	IncludeFiles   *bool    `json:"include_files,omitempty"`
	IncludeFolders *bool    `json:"include_folders,omitempty"`
	NamePattern    string   `json:"name_pattern,omitempty"`
	Extensions     []string `json:"extensions,omitempty"`
	MinSize        int64    `json:"min_size,omitempty"`
	MaxSize        int64    `json:"max_size,omitempty"`
	ModifiedAfter  string   `json:"modified_after,omitempty"`
	ModifiedBefore string   `json:"modified_before,omitempty"`
	SortBy         string   `json:"sort_by,omitempty"`
	SortOrder      string   `json:"sort_order,omitempty"`
	Cursor         int      `json:"cursor,omitempty"`
	Limit          int      `json:"limit,omitempty"`
}

type ListResult struct {
	ReadID          string  `json:"read_id"`
	Path            string  `json:"path"`
	Entries         []Entry `json:"entries"`
	NextCursor      int     `json:"next_cursor,omitempty"`
	Truncated       bool    `json:"truncated"`
	ScannedEntries  int     `json:"scanned_entries"`
	SkippedSymlinks int     `json:"skipped_symlinks"`
}

type TreeParams struct {
	ReadID        string `json:"read_id,omitempty"`
	Path          string `json:"path,omitempty"`
	Depth         int    `json:"depth,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
	IncludeFiles  *bool  `json:"include_files,omitempty"`
	MaxEntries    int    `json:"max_entries,omitempty"`
}

type TreeNode struct {
	Path       string      `json:"path"`
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Size       int64       `json:"size,omitempty"`
	Mode       string      `json:"mode"`
	ModifiedAt string      `json:"modified_at"`
	Children   []*TreeNode `json:"children,omitempty"`
}

type TreeResult struct {
	ReadID          string    `json:"read_id"`
	Root            *TreeNode `json:"root"`
	EntryCount      int       `json:"entry_count"`
	ScannedEntries  int       `json:"scanned_entries"`
	SkippedSymlinks int       `json:"skipped_symlinks"`
	Truncated       bool      `json:"truncated"`
}

type SummaryParams struct {
	ReadID        string `json:"read_id,omitempty"`
	Path          string `json:"path,omitempty"`
	Depth         int    `json:"depth,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
}

type SummaryResult struct {
	ReadID           string         `json:"read_id"`
	Path             string         `json:"path"`
	FileCount        int            `json:"file_count"`
	FolderCount      int            `json:"folder_count"`
	OtherCount       int            `json:"other_count"`
	TotalBytes       int64          `json:"total_bytes"`
	MaxDepth         int            `json:"max_depth"`
	EmptyFolders     int            `json:"empty_folders"`
	Extensions       map[string]int `json:"extensions"`
	LargestFile      *Entry         `json:"largest_file,omitempty"`
	RecentlyModified *Entry         `json:"recently_modified,omitempty"`
	ScannedEntries   int            `json:"scanned_entries"`
	SkippedSymlinks  int            `json:"skipped_symlinks"`
	Truncated        bool           `json:"truncated"`
}

type SnapshotParams struct {
	ReadID            string `json:"read_id,omitempty"`
	Path              string `json:"path,omitempty"`
	Depth             int    `json:"depth,omitempty"`
	IncludeHidden     bool   `json:"include_hidden,omitempty"`
	IncludeFileHashes bool   `json:"include_file_hashes,omitempty"`
	Limit             int    `json:"limit,omitempty"`
}

type SnapshotResult struct {
	ReadID          string  `json:"read_id"`
	SnapshotID      string  `json:"snapshot_id"`
	Path            string  `json:"path"`
	Digest          string  `json:"digest"`
	Entries         []Entry `json:"entries"`
	EntryCount      int     `json:"entry_count"`
	ScannedEntries  int     `json:"scanned_entries"`
	SkippedSymlinks int     `json:"skipped_symlinks"`
	Truncated       bool    `json:"truncated"`
}

type CompareParams struct {
	ReadID            string  `json:"read_id,omitempty"`
	Path              string  `json:"path,omitempty"`
	Depth             int     `json:"depth,omitempty"`
	IncludeHidden     bool    `json:"include_hidden,omitempty"`
	IncludeFileHashes bool    `json:"include_file_hashes,omitempty"`
	PreviousEntries   []Entry `json:"previous_entries"`
}

type CompareResult struct {
	ReadID          string   `json:"read_id"`
	CurrentDigest   string   `json:"current_digest"`
	Added           []string `json:"added"`
	Removed         []string `json:"removed"`
	Modified        []string `json:"modified"`
	UnchangedCount  int      `json:"unchanged_count"`
	ScannedEntries  int      `json:"scanned_entries"`
	SkippedSymlinks int      `json:"skipped_symlinks"`
	Truncated       bool     `json:"truncated"`
}

type ReadRequest struct {
	Kind     string          `json:"kind"`
	Stat     *StatParams     `json:"stat,omitempty"`
	List     *ListParams     `json:"list,omitempty"`
	Tree     *TreeParams     `json:"tree,omitempty"`
	Summary  *SummaryParams  `json:"summary,omitempty"`
	Snapshot *SnapshotParams `json:"snapshot,omitempty"`
	Compare  *CompareParams  `json:"compare,omitempty"`
}

type BatchParams struct {
	Reads []ReadRequest `json:"reads"`
}

type BatchItem struct {
	Kind     string          `json:"kind"`
	Stat     *FolderStat     `json:"stat,omitempty"`
	List     *ListResult     `json:"list,omitempty"`
	Tree     *TreeResult     `json:"tree,omitempty"`
	Summary  *SummaryResult  `json:"summary,omitempty"`
	Snapshot *SnapshotResult `json:"snapshot,omitempty"`
	Compare  *CompareResult  `json:"compare,omitempty"`
}

type BatchResult struct {
	Results []BatchItem `json:"results"`
}

type CancelParams struct {
	ReadID string `json:"read_id"`
}

type CancelResult struct {
	ReadID   string `json:"read_id"`
	Canceled bool   `json:"canceled"`
}
