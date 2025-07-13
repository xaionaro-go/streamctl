package action

import "github.com/xaionaro-go/serializable"

func init() {
	serializable.RegisterType[Noop]()
}

type Noop struct{}

var _ Action = (*Noop)(nil)

func (*Noop) isAction() {}

func (a *Noop) String() string {
	if a == nil {
		return "null"
	}
	return string(tryJSON(*a))
}
