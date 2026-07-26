package writepolicy

import "context"

type Operation struct{ Kind, Path string }
type Policy interface {
	Authorize(context.Context, Operation) error
}
type AllowAll struct{}

func (AllowAll) Authorize(context.Context, Operation) error { return nil }
