package repository

import (
	"github.com/google/go-github/v62/github"
)

type GitHub struct {
	Client *github.Client
}

func NewGitHub() *GitHub {
	client := github.NewClient(nil).WithAuthToken("... your access token ...")
	return &GitHub{
		Client: client,
	}
}
