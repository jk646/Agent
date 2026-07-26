package protocol

import "encoding/json"

const Version = "1"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	ParseError        = -32700
	InvalidRequest    = -32600
	MethodNotFound    = -32601
	InvalidParams     = -32602
	InternalError     = -32603
	ErrConflict       = -32001
	ErrNotFound       = -32002
	ErrBusy           = -32003
	ErrCapacity       = -32004
	ErrPolicyRejected = -32005

	ErrFileOutsideWorkspace = -32100
	ErrFileStale            = -32101
	ErrFileMatchNotFound    = -32102
	ErrFileMatchCount       = -32103
	ErrFileTooLarge         = -32104
	ErrFileBinary           = -32105
	ErrFileAlreadyExists    = -32106
	ErrFileSymlink          = -32107
	ErrFileRollbackConflict = -32108
	ErrFileInvalidOperation = -32109

	ErrSearchOutsideWorkspace = -32200
	ErrSearchSymlink          = -32201
	ErrSearchInvalidRequest   = -32202
	ErrSearchTooLarge         = -32203
	ErrSearchCapacity         = -32204
	ErrSearchDuplicateID      = -32205
	ErrSearchNotActive        = -32206
	ErrSearchCanceled         = -32207

	ErrReadOutsideWorkspace    = -32300
	ErrReadSymlink             = -32301
	ErrReadInvalidRequest      = -32302
	ErrReadTooLarge            = -32303
	ErrReadUnsupportedEncoding = -32304
	ErrReadCapacity            = -32305
	ErrReadDuplicateID         = -32306
	ErrReadNotActive           = -32307
	ErrReadCanceled            = -32308
	ErrReadFileChanged         = -32309
	ErrReadNotRegular          = -32310

	ErrFolderOutsideWorkspace = -32400
	ErrFolderSymlink          = -32401
	ErrFolderInvalidRequest   = -32402
	ErrFolderTooLarge         = -32403
	ErrFolderCapacity         = -32404
	ErrFolderDuplicateID      = -32405
	ErrFolderNotActive        = -32406
	ErrFolderCanceled         = -32407
	ErrFolderNotFolder        = -32408

	ErrTextSearchOutsideWorkspace = -32500
	ErrTextSearchSymlink          = -32501
	ErrTextSearchInvalidRequest   = -32502
	ErrTextSearchLimit            = -32503
	ErrTextSearchEncoding         = -32504
	ErrTextSearchCapacity         = -32505
	ErrTextSearchDuplicateID      = -32506
	ErrTextSearchNotActive        = -32507
	ErrTextSearchCanceled         = -32508

	ErrWriteOutsideWorkspace = -32520
	ErrWriteSymlink          = -32521
	ErrWriteInvalidRequest   = -32522
	ErrWriteTooLarge         = -32523
	ErrWriteStale            = -32524
	ErrWriteAlreadyExists    = -32525
	ErrWriteUnsupportedType  = -32526
	ErrWriteCapacity         = -32527
	ErrWriteDuplicateID      = -32528
	ErrWriteNotActive        = -32529
	ErrWriteRollbackConflict = -32530
	ErrWriteTransaction      = -32531
	ErrWriteCanceled         = -32532
)

func NewResponse(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}
func NewError(id json.RawMessage, code int, message string, data any) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message, Data: data}}
}
