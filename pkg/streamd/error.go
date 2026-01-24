package streamd

import "errors"

var ErrSkipBackend = errors.New("backend was skipped")

type ErrNoVariable struct{}

var _ error = ErrNoVariable{}

func (ErrNoVariable) Error() string { return "no such variable" }

type ErrVariableWrongType struct{}

var _ error = ErrVariableWrongType{}

func (ErrVariableWrongType) Error() string { return "wrong variable type" }
