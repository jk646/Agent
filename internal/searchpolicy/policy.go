package searchpolicy

import "context"

type Operation struct {
	Kind string
	Path string
}

type Policy interface {
	Authorize(context.Context, Operation) error
}

type AllowAll struct{}

func (AllowAll) Authorize(context.Context, Operation) error { return nil }
