package orchestrator

import "errors"

var (
	ErrInvalidConfig     = errors.New("invalid orchestrator configuration")
	ErrInvalidRequest    = errors.New("invalid orchestrator request")
	ErrToolNotFound      = errors.New("tool was not found")
	ErrMethodNotRoutable = errors.New("method cannot be routed to exactly one tool")
	ErrToolUnavailable   = errors.New("tool is unavailable")
	ErrCapacity          = errors.New("orchestrator capacity reached")
)
