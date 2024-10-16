package repository

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
	"golang.org/x/crypto/ssh"
)

type GIT struct {
	RemoteURL      string
	PrivateKey     []byte
	Auth           transport.AuthMethod
	Repo           *git.Repository
	FilePath       string
	CommitterName  string
	CommitterEmail string
}

func NewGit(
	ctx context.Context,
	remoteURL string,
	privateKey []byte,
	filePath string,
	committerName string,
	committerEmail string,
) (*GIT, error) {
	stor := &GIT{
		RemoteURL:      remoteURL,
		PrivateKey:     privateKey,
		FilePath:       filePath,
		CommitterName:  committerName,
		CommitterEmail: committerEmail,
	}

	err := stor.init(ctx)
	if err != nil {
		return nil, err
	}

	return stor, nil
}

func (g *GIT) init(ctx context.Context) error {
	if len(g.RemoteURL) == 0 {
		return fmt.Errorf("repo URL is not provided")
	}
	if len(g.PrivateKey) == 0 {
		return fmt.Errorf("key is not provided")
	}
	auth, err := gitssh.NewPublicKeys("git", g.PrivateKey, "")
	if err != nil {
		return fmt.Errorf("unable to create auth object for git: %w", err)
	}
	logger.Tracef(ctx, "auth: %#+v", auth)
	auth.HostKeyCallbackHelper.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return nil
	}
	g.Auth = auth

	repo, err := git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
		URL:               g.RemoteURL,
		Auth:              g.Auth,
		SingleBranch:      true,
		Mirror:            true,
		NoCheckout:        true,
		Depth:             0,
		RecurseSubmodules: 0,
		Progress:          nil,
		Tags:              0,
	})
	switch err {
	case nil:
	case plumbing.ErrReferenceNotFound, transport.ErrEmptyRemoteRepository:
		repo, err = g.initGitRepo(ctx)
		if err != nil {
			return fmt.Errorf("unable to initialize the git repo: %w", err)
		}
	default:
		return fmt.Errorf("unable to clone the git repo: %w", err)
	}

	g.Repo = repo
	return nil
}

type ErrNeedsRebase struct {
	Err error
}

func (err ErrNeedsRebase) Error() string {
	return fmt.Sprintf("needs rebase: %v", err.Err)
}

func (err ErrNeedsRebase) Unwrap() error {
	return err.Err
}

func (g *GIT) initGitRepo(ctx context.Context) (*git.Repository, error) {
	repo, err := git.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the git local repo: %w", err)
	}

	if err := g.initLocalBranch(ctx, repo); err != nil {
		return nil, fmt.Errorf("unable to make a local branch: %w", err)
	}
	if err := g.push(ctx, repo); err != nil {
		return nil, fmt.Errorf("unable to push the branch to the remote: %w", err)
	}

	return repo, nil
}

func (g *GIT) initLocalBranch(
	_ context.Context,
	repo *git.Repository,
) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to get git's worktree: %w", err)
	}

	now := time.Now()
	signature := g.gitCommitter(now)

	_, err = worktree.Commit("init repo", &git.CommitOptions{
		All:               false,
		AllowEmptyCommits: true,
		Author:            signature,
		Committer:         signature,
		Amend:             false,
	})
	if err != nil {
		return fmt.Errorf("unable to create the first commit in the git repo: %w", err)
	}
	return nil
}

func (g *GIT) push(
	ctx context.Context,
	repo interface {
		PushContext(context.Context, *git.PushOptions) error
	},
) error {
	err := repo.PushContext(ctx, &git.PushOptions{
		RemoteURL:         g.RemoteURL,
		Auth:              g.Auth,
		Progress:          nil,
		Prune:             false,
		Force:             false,
		InsecureSkipTLS:   false,
		CABundle:          nil,
		RequireRemoteRefs: nil,
		FollowTags:        false,
		ForceWithLease:    nil,
		Options:           nil,
		Atomic:            false,
	})
	logger.Debugf(ctx, "push result: %v", err)
	if err == git.NoErrAlreadyUpToDate {
		return nil
	}
	if err != nil && strings.Contains(err.Error(), "is at") &&
		strings.Contains(err.Error(), "but expected") {
		return ErrNeedsRebase{Err: err}
	}
	if err != nil {
		return err
	}

	return nil
}

func (g *GIT) Read() ([]byte, error) {
	worktree, err := g.Repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("unable to open the git's worktree: %w", err)
	}

	f, err := worktree.Filesystem.Open(g.FilePath)
	if err != nil {
		return nil, fmt.Errorf("unable to open '%s': %w", g.FilePath, err)
	}

	b, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf(
			"unable to read the content of file '%s' from the virtual git repository: %w",
			g.FilePath,
			err,
		)
	}

	return b, nil
}

func (g *GIT) Pull(
	ctx context.Context,
	lastKnownCommitHash plumbing.Hash,
	onUpdate func(
		ctx context.Context,
		commitHash plumbing.Hash,
		newData []byte,
	),
) (_err error) {
	logger.Debugf(ctx, "gitStorage.Pull")
	defer func() { logger.Debugf(ctx, "/gitStorage.Pull: %v", _err) }()

	err := g.Repo.FetchContext(ctx, &git.FetchOptions{
		RemoteURL: g.RemoteURL,
		Auth:      g.Auth,
		Force:     true,
		Prune:     false,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("unable to fetch from the remote git repo: %w", err)
	}
	worktree, err := g.Repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to open the git's worktree: %w", err)
	}
	if err != git.NoErrAlreadyUpToDate {
		err = worktree.PullContext(ctx, &git.PullOptions{
			RemoteURL:         g.RemoteURL,
			SingleBranch:      true,
			Depth:             0,
			Auth:              g.Auth,
			RecurseSubmodules: 0,
			Progress:          nil,
			Force:             true,
			InsecureSkipTLS:   false,
			CABundle:          nil,
			ProxyOptions:      transport.ProxyOptions{},
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return fmt.Errorf("unable to pull the updates in the git repo: %w", err)
		}
	}

	ref, err := g.Repo.Head()
	if err != nil {
		return fmt.Errorf("unable to get the current git ref: %w", err)
	}
	newCommitHash := ref.Hash()

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash: newCommitHash,
	})
	if err != nil {
		return fmt.Errorf("unable to checkout HEAD: %w", err)
	}

	newData, err := g.Read()
	if err != nil {
		return fmt.Errorf("unable to parse config from the git source: %w", err)
	}

	if lastKnownCommitHash == newCommitHash {
		logger.Debugf(ctx, "git is already in sync: %s == %s", lastKnownCommitHash, newCommitHash)
		return nil
	}
	logger.Debugf(
		ctx,
		"got a different commit from git: %s != %s",
		lastKnownCommitHash,
		newCommitHash,
	)

	oldCommit, _ := g.Repo.CommitObject(newCommitHash)
	if oldCommit != nil {
		logger.Debugf(
			ctx,
			"we already have this commit in the history on our side, skipping it: %s: %#+v",
			newCommitHash,
			oldCommit,
		)
		return nil
	}

	onUpdate(ctx, newCommitHash, newData)
	return nil
}

func (g *GIT) gitCommitter(now time.Time) *object.Signature {
	return &object.Signature{
		Name:  g.CommitterName,
		Email: g.CommitterEmail,
		When:  now,
	}
}

func (g *GIT) CommitAndPush(
	ctx context.Context,
	worktree *git.Worktree,
	ref *plumbing.Reference,
) (plumbing.Hash, error) {
	logger.Debugf(ctx, "gitStorage.CommitAndPush")
	defer logger.Debugf(ctx, "/gitStorage.CommitAndPush")

	_, err := worktree.Add(g.FilePath)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf(
			"unable to add file '%s' to the git's worktree: %w",
			g.FilePath,
			err,
		)
	}

	now := time.Now()
	signature := g.gitCommitter(now)
	ts := now.Format(time.DateTime)
	host, err := os.Hostname()
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to determine the host name: %w", err)
	}

	hash, err := worktree.Commit(
		fmt.Sprintf("Update from '%s' at %s", host, ts),
		&git.CommitOptions{
			All:               true,
			AllowEmptyCommits: false,
			Author:            signature,
			Committer:         signature,
		},
	)
	if err != nil {
		return hash, fmt.Errorf("unable to commit the new config to the git repo: %w", err)
	}
	logger.Debugf(ctx, "new commit: %s", hash)

	newRef := plumbing.NewHashReference(ref.Name(), hash)
	err = g.Repo.Storer.SetReference(newRef)
	if err != nil {
		return hash, fmt.Errorf("unable to set git reference %#+v: %w", *newRef, err)
	}

	logger.Debugf(ctx, "had set reference: %#+v", *newRef)

	err = g.push(ctx, g.Repo)
	if err != nil {
		return hash, fmt.Errorf("unable to push the new config to the remote git repo: %w", err)
	}

	return hash, nil
}

func (g *GIT) Close() error {
	return nil
}

func (g *GIT) Write(
	ctx context.Context,
	b []byte,
) (plumbing.Hash, error) {
	logger.Debugf(ctx, "Write")
	defer logger.Debugf(ctx, "/Write")

	worktree, err := g.Repo.Worktree()
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to open the git's worktree: %w", err)
	}

	refs, err := g.Repo.References()
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to get git references iterator: %w", err)
	}
	var ref *plumbing.Reference
	for {
		ref, err = refs.Next()
		if err != nil {
			return plumbing.Hash{}, fmt.Errorf("unable to get the first git reference: %w", err)
		}
		if ref.Name() != "HEAD" {
			break
		}
	}

	if ref == nil {
		return plumbing.Hash{}, fmt.Errorf("unable to find any branch references")
	}

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash:  ref.Hash(),
		Force: true,
	})
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to checkout git HEAD: %w", err)
	}

	f, err := worktree.Filesystem.Open(
		g.FilePath,
	)
	logger.Debugf(ctx, "file open result: %v", err)
	if err != nil && !os.IsNotExist(err) {
		return plumbing.Hash{}, fmt.Errorf(
			"unable to open file '%s' for reading: %w",
			g.FilePath,
			err,
		)
	}

	var sha1SumBefore [sha1.Size]byte

	if f != nil {
		b, err := io.ReadAll(f)
		if err != nil {
			return plumbing.Hash{}, fmt.Errorf("unable to read file '%s': %w", g.FilePath, err)
		}
		f.Close()
		sha1SumBefore = sha1.Sum(b)
	} else {
		logger.Debugf(ctx, "the file does not exist in the git repo, yet")
	}

	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to encode the config: %w", err)
	}

	sha1SumAfter := sha1.Sum(b)
	if bytes.Equal(sha1SumBefore[:], sha1SumAfter[:]) {
		logger.Debugf(ctx, "the config didn't change: %X == %X", sha1SumBefore[:], sha1SumAfter[:])
		return plumbing.Hash{}, nil
	}
	logger.Debugf(ctx, "the config did change: %X == %X", sha1SumBefore[:], sha1SumAfter[:])

	f, err = worktree.Filesystem.OpenFile(
		g.FilePath,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0644,
	)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf(
			"unable to open file '%s' for writing: %w",
			g.FilePath,
			err,
		)
	}
	_, err = io.Copy(f, bytes.NewReader(b))
	f.Close()
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf(
			"unable to write the config into virtual git repo: %w",
			err,
		)
	}

	hash, err := g.CommitAndPush(ctx, worktree, ref)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to Commit&Push: %w", err)
	}

	return hash, nil
}
