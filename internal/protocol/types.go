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
)

func NewResponse(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}
func NewError(id json.RawMessage, code int, message string, data any) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message, Data: data}}
}
