package filereader

type Config struct {
	Workspace     string
	MaxTextBytes  int64
	MaxChunkBytes int
	MaxHashBytes  int64
	MaxConcurrent int
	MaxBatchItems int
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
	Encoding   string `json:"encoding,omitempty"`
}

type TextParams struct {
	ReadID    string `json:"read_id,omitempty"`
	Path      string `json:"path"`
	StartChar int    `json:"start_char,omitempty"`
	EndChar   int    `json:"end_char,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type TextResult struct {
	ReadID     string `json:"read_id"`
	Path       string `json:"path"`
	Content    string `json:"content"`
	Encoding   string `json:"encoding"`
	Newline    string `json:"newline"`
	SHA256     string `json:"sha256"`
	StartChar  int    `json:"start_char"`
	EndChar    int    `json:"end_char"`
	TotalChars int    `json:"total_chars"`
	NextChar   int    `json:"next_char,omitempty"`
	Truncated  bool   `json:"truncated"`
}

type LinesParams struct {
	ReadID             string `json:"read_id,omitempty"`
	Path               string `json:"path"`
	StartLine          int    `json:"start_line,omitempty"`
	EndLine            int    `json:"end_line,omitempty"`
	MaxBytes           int    `json:"max_bytes,omitempty"`
	IncludeLineNumbers bool   `json:"include_line_numbers,omitempty"`
}

type LinesResult struct {
	ReadID     string `json:"read_id"`
	Path       string `json:"path"`
	Content    string `json:"content"`
	Encoding   string `json:"encoding"`
	Newline    string `json:"newline"`
	SHA256     string `json:"sha256"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	TotalLines int    `json:"total_lines"`
	NextLine   int    `json:"next_line,omitempty"`
	Truncated  bool   `json:"truncated"`
}

type BytesParams struct {
	ReadID      string `json:"read_id,omitempty"`
	Path        string `json:"path"`
	Offset      int64  `json:"offset,omitempty"`
	Length      int    `json:"length,omitempty"`
	IncludeHash bool   `json:"include_hash,omitempty"`
}

type BytesResult struct {
	ReadID     string `json:"read_id"`
	Path       string `json:"path"`
	Offset     int64  `json:"offset"`
	BytesRead  int    `json:"bytes_read"`
	DataBase64 string `json:"data_base64"`
	NextOffset int64  `json:"next_offset,omitempty"`
	EOF        bool   `json:"eof"`
	SHA256     string `json:"sha256,omitempty"`
}

type HashParams struct {
	ReadID string `json:"read_id,omitempty"`
	Path   string `json:"path"`
}

type HashResult struct {
	ReadID string `json:"read_id"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ReadRequest struct {
	Kind  string       `json:"kind"`
	Stat  *StatParams  `json:"stat,omitempty"`
	Text  *TextParams  `json:"text,omitempty"`
	Lines *LinesParams `json:"lines,omitempty"`
	Bytes *BytesParams `json:"bytes,omitempty"`
	Hash  *HashParams  `json:"hash,omitempty"`
}

type BatchParams struct {
	Reads []ReadRequest `json:"reads"`
}

type BatchItem struct {
	Kind  string       `json:"kind"`
	Stat  *FileInfo    `json:"stat,omitempty"`
	Text  *TextResult  `json:"text,omitempty"`
	Lines *LinesResult `json:"lines,omitempty"`
	Bytes *BytesResult `json:"bytes,omitempty"`
	Hash  *HashResult  `json:"hash,omitempty"`
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
