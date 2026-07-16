package policy

import (
	"context"
	"errors"
)

var ErrRejected = errors.New("execution policy rejected")

type Request struct {
	Command string
	Cwd     string
	Env     map[string]string
	Shell   string
}
type ExecutionPolicy interface {
	Authorize(context.Context, Request) error
}
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, Request) error { return nil }
