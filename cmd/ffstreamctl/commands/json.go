package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

func jsonOutput(
	ctx context.Context,
	out io.Writer,
	obj any,
) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", " ")
	err := enc.Encode(obj)
	assertNoError(ctx, err)

	out.Write(buf.Bytes())
}

func jsonInput[T any](
	ctx context.Context,
	in io.Reader,
) T {
	dec := json.NewDecoder(in)
	var result T
	err := dec.Decode(&result)
	assertNoError(ctx, err)
	return result
}
