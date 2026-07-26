package filewriter

import "time"

type Config struct {
	Workspace, TempDir string
	MaxFileBytes       int64
	MaxBatchFiles      int
	MaxBatchBytes      int64
	MaxRollbackBytes   int64
	MaxConcurrent      int
	JournalTTL         time.Duration
}

type Operation struct {
	WriteID         string `json:"write_id,omitempty"`
	Kind            string `json:"kind,omitempty"`
	Path            string `json:"path"`
	Content         string `json:"content,omitempty"`
	DataBase64      string `json:"data_base64,omitempty"`
	ExpectedSHA256  string `json:"expected_sha256,omitempty"`
	CreateParents   bool   `json:"create_parents,omitempty"`
	CreateIfMissing bool   `json:"create_if_missing,omitempty"`
	AddNewline      bool   `json:"add_newline,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Offset          int64  `json:"offset,omitempty"`
	AllowSparse     bool   `json:"allow_sparse,omitempty"`
}

type BatchParams struct {
	WriteID    string      `json:"write_id,omitempty"`
	Preview    bool        `json:"preview,omitempty"`
	Operations []Operation `json:"operations"`
}
type FileChange struct {
	Path          string `json:"path"`
	Action        string `json:"action"`
	Size          int64  `json:"size"`
	BeforeSHA256  string `json:"before_sha256,omitempty"`
	AfterSHA256   string `json:"after_sha256,omitempty"`
	Diff          string `json:"diff,omitempty"`
	DiffTruncated bool   `json:"diff_truncated,omitempty"`
}
type Result struct {
	WriteID           string       `json:"write_id"`
	TransactionID     string       `json:"transaction_id,omitempty"`
	Applied           bool         `json:"applied"`
	Preview           bool         `json:"preview"`
	RollbackAvailable bool         `json:"rollback_available"`
	Files             []FileChange `json:"files"`
}
type RollbackParams struct {
	TransactionID string `json:"transaction_id"`
}
type RollbackResult struct {
	TransactionID string       `json:"transaction_id"`
	RolledBack    bool         `json:"rolled_back"`
	Files         []FileChange `json:"files"`
}
type CancelParams struct {
	WriteID string `json:"write_id"`
}
type CancelResult struct {
	WriteID  string `json:"write_id"`
	Canceled bool   `json:"canceled"`
}
