package streampanel

import (
	"context"
	"fmt"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-andiamo/splitter"
	"github.com/xaionaro-go/streamctl/pkg/expression"
)

var commandSplitter splitter.Splitter

func init() {
	commandSplitter = splitter.MustCreateSplitter(
		' ',
		splitter.DoubleQuotesBackSlashEscaped,
	).AddDefaultOptions(
		splitter.Trim(" \t\r\n"),
		splitter.IgnoreEmpties,
		splitter.UnescapeQuotes,
	)
}

func expandCommand(
	ctx context.Context,
	cmdString string,
	context any,
) ([]string, error) {
	cmdStringExpanded, err := expression.Eval[string](expression.Expression(cmdString), context)
	if err != nil {
		return nil, err
	}

	logger.Debugf(ctx, "expanded command is: <%s>", cmdStringExpanded)

	args, err := commandSplitter.Split(cmdStringExpanded)
	if err != nil {
		return nil, fmt.Errorf("unable to split '%s': %w", cmdStringExpanded, err)
	}

	return args, nil
}
