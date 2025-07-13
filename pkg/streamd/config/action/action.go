package action

import (
	"fmt"

	"github.com/xaionaro-go/streamctl/pkg/expression"
)

type Action interface {
	fmt.Stringer
	isAction() // just to enable build-time type checks
}

type ValueExpression = expression.Expression
