package ui

import "context"

type UI interface {
	DisplayError(error)
	Restart(context.Context, string)
	InputGitUserData(
		ctx context.Context,
	) (bool, string, []byte, error)
}
