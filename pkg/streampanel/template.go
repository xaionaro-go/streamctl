package streampanel

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/template"
)

var funcMap = map[string]interface{}{
	"devnull": func(args ...any) string {
		return ""
	},
	"httpGET": func(urlString string) string {
		resp, err := http.Get(urlString)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(err)
		}

		return string(b)
	},
}

func expandTemplate(tpl string) (string, error) {
	parsed, err := template.New("").Funcs(funcMap).Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("unable to parse the template: %w", err)
	}

	var buf bytes.Buffer
	if err = parsed.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("unable to execute the template: %w", err)
	}

	return buf.String(), nil
}

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
	cmdStringExpanded, err := expandTemplate(cmdString)
	if err != nil {
		return nil, err
	}

	cmdStringExpandedClean := strings.Trim(cmdStringExpanded, " \t\r\n")
	if len(cmdStringExpandedClean) == 0 {
		return nil, nil
	}

	return splitWithQuotes(cmdStringExpandedClean), nil
}
