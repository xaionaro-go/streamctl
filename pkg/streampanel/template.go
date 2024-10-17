package streampanel

import (
	"strings"

	"github.com/xaionaro-go/streamctl/pkg/expression"
)

func splitWithQuotes(s string) []string {
	var result []string
	var current string
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]

		if inQuotes {
			if c == quoteChar {
				inQuotes = false
				quoteChar = 0
			} else {
				current += string(c)
			}
		} else {
			switch c {
			case ' ', '\t':
				if len(current) > 0 {
					result = append(result, current)
					current = ""
				}
			case '\'', '"':
				inQuotes = true
				quoteChar = c
			default:
				current += string(c)
			}
		}
	}

	if len(current) > 0 {
		result = append(result, current)
	}

	return result
}

func expandCommand(cmdString string) ([]string, error) {
	cmdStringExpanded, err := expression.Eval[string](expression.Expression(cmdString), nil)
	if err != nil {
		return nil, err
	}

	cmdStringExpandedClean := strings.Trim(cmdStringExpanded, " \t\r\n")
	if len(cmdStringExpandedClean) == 0 {
		return nil, nil
	}

	return splitWithQuotes(cmdStringExpandedClean), nil
}
