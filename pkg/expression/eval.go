package expression

import (
	"bytes"
	"fmt"
	"text/template"
)

func Eval[T any](
	expr Expression,
	evalCtx any,
) (T, error) {
	var result T

	tmpl, err := template.New("").Funcs(funcMap).Parse(string(expr))
	if err != nil {
		return result, fmt.Errorf("unable to parse the template: %w", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, evalCtx)
	if err != nil {
		return result, fmt.Errorf("unable to execute the template: %w", err)
	}

	value := buf.String()
	_, err = fmt.Sscanf(value, "%v", &result)
	if err != nil {
		return result, fmt.Errorf("unable to scan value '%v' into %T: %w", value, result, err)
	}

	return result, nil
}
