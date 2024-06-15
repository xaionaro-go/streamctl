package streampanel

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
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

const gitRepoPanelConfigFileName = "streampanel.yaml"

type gitStorage struct {
	RemoteURL  string
	PrivateKey []byte
	Auth       transport.AuthMethod
	Repo       *git.Repository
}

func newGitStorage(
	ctx context.Context,
	remoteURL string,
	privateKey []byte,
) (*gitStorage, error) {
	stor := &gitStorage{
		RemoteURL:  remoteURL,
		PrivateKey: privateKey,
	}

	err := stor.init(ctx)
	if err != nil {
		return nil, err
	}

	return stor, nil
}

func (s *gitStorage) init(ctx context.Context) error {
	if len(s.RemoteURL) == 0 {
		return fmt.Errorf("repo URL is not provided")
	}
	if len(s.PrivateKey) == 0 {
		return fmt.Errorf("key is not provided")
	}
	auth, err := gitssh.NewPublicKeys("git", s.PrivateKey, "")
	if err != nil {
		return fmt.Errorf("unable to create auth object for git: %w", err)
	}
	auth.HostKeyCallbackHelper.HostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		return nil
	}
	s.Auth = auth

	repo, err := git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
		URL:               s.RemoteURL,
		Auth:              s.Auth,
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
		repo, err = s.initGitRepo(ctx)
		if err != nil {
			return fmt.Errorf("unable to initialize the git repo: %w", err)
		}
	default:
		return fmt.Errorf("unable to clone the git repo: %w", err)
	}

	s.Repo = repo
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

func (s *gitStorage) initGitRepo(ctx context.Context) (*git.Repository, error) {
	repo, err := git.Init(memory.NewStorage(), memfs.New())
	if err != nil {
		return nil, fmt.Errorf("unable to initialize the git local repo: %w", err)
	}

	if err := s.initLocalBranch(ctx, repo); err != nil {
		return nil, fmt.Errorf("unable to make a local branch: %w", err)
	}
	if err := s.push(ctx, repo); err != nil {
		return nil, fmt.Errorf("unable to push the branch to the remote: %w", err)
	}

	return repo, nil
}

func (s *gitStorage) initLocalBranch(
	_ context.Context,
	repo *git.Repository,
) error {
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to get git's worktree: %w", err)
	}

	now := time.Now()
	signature := gitCommitter(now)

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

func (s *gitStorage) push(
	ctx context.Context,
	repo interface {
		PushContext(context.Context, *git.PushOptions) error
	},
) error {
	err := repo.PushContext(ctx, &git.PushOptions{
		RemoteURL:         s.RemoteURL,
		Auth:              s.Auth,
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
	if err != nil && strings.Contains(err.Error(), "is at") && strings.Contains(err.Error(), "but expected") {
		return ErrNeedsRebase{Err: err}
	}
	if err != nil {
		return err
	}

	return nil
}

func (s *gitStorage) readConfig(ctx context.Context) (*panelData, error) {
	worktree, err := s.Repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("unable to open the git's worktree: %w", err)
	}

	f, err := worktree.Filesystem.Open(gitRepoPanelConfigFileName)
	if err != nil {
		return nil, fmt.Errorf("unable to open '%s': %w", gitRepoPanelConfigFileName, err)
	}

	b, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("unable to read the content of file '%s' from the virtual git repository: %w", gitRepoPanelConfigFileName, err)
	}

	panelData := newPanelData()
	err = readPanelData(ctx, b, &panelData)

	if err != nil {
		return nil, fmt.Errorf("unable to parse the config: %w", err)
	}

	return &panelData, nil
}

func (s *gitStorage) Pull(
	ctx context.Context,
	lastKnownCommit plumbing.Hash,
	onUpdate func(
		ctx context.Context,
		newData *panelData,
	),
) (_err error) {
	logger.Debugf(ctx, "gitStorage.Pull")
	defer func() { logger.Debugf(ctx, "/gitStorage.Pull: %v", _err) }()

	err := s.Repo.FetchContext(ctx, &git.FetchOptions{
		RemoteURL: s.RemoteURL,
		Auth:      s.Auth,
		Force:     true,
		Prune:     false,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("unable to fetch from the remote git repo: %w", err)
	}
	worktree, err := s.Repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to open the git's worktree: %w", err)
	}
	if err != git.NoErrAlreadyUpToDate {
		err = worktree.PullContext(ctx, &git.PullOptions{
			RemoteURL:         s.RemoteURL,
			SingleBranch:      true,
			Depth:             0,
			Auth:              s.Auth,
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

	ref, err := s.Repo.Head()
	if err != nil {
		return fmt.Errorf("unable to get the current git ref: %w", err)
	}
	newCommit := ref.Hash()

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash: newCommit,
	})
	if err != nil {
		return fmt.Errorf("unable to checkout HEAD: %w", err)
	}

	newData, err := s.readConfig(ctx)
	if err != nil {
		return fmt.Errorf("unable to parse config from the git source: %w", err)
	}

	if lastKnownCommit == newCommit {
		logger.Debugf(ctx, "git is already in sync: %s == %s", lastKnownCommit, newCommit)
		return nil
	}

	logger.Debugf(ctx, "got a new commit from git: %s -> %s", lastKnownCommit, newCommit)
	newData.GitRepo.LatestSyncCommit = newCommit.String()
	onUpdate(ctx, newData)
	return nil
}

func gitCommitter(now time.Time) *object.Signature {
	return &object.Signature{
		Name:  "streampanel",
		Email: "streampanel@noemail.invalid",
		When:  now,
	}
}

func (s *gitStorage) CommitAndPush(
	ctx context.Context,
	worktree *git.Worktree,
	ref *plumbing.Reference,
) (plumbing.Hash, error) {
	logger.Debugf(ctx, "gitStorage.CommitAndPush")
	defer logger.Debugf(ctx, "/gitStorage.CommitAndPush")

	_, err := worktree.Add(gitRepoPanelConfigFileName)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to add file '%s' to the git's worktree: %w", gitRepoPanelConfigFileName, err)
	}

	now := time.Now()
	signature := gitCommitter(now)
	ts := now.Format(time.DateTime)
	host, err := os.Hostname()
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("unable to determine the host name: %w", err)
	}

	hash, err := worktree.Commit(fmt.Sprintf("Update from '%s' at %s", host, ts), &git.CommitOptions{
		All:               true,
		AllowEmptyCommits: false,
		Author:            signature,
		Committer:         signature,
	})
	if err != nil {
		return hash, fmt.Errorf("unable to commit the new config to the git repo: %w", err)
	}
	logger.Debugf(ctx, "new commit: %s", hash)

	newRef := plumbing.NewHashReference(ref.Name(), hash)
	err = s.Repo.Storer.SetReference(newRef)
	if err != nil {
		return hash, fmt.Errorf("unable to set git reference %#+v: %w", *newRef, err)
	}

	logger.Debugf(ctx, "had set reference: %#+v", *newRef)

	err = s.push(ctx, s.Repo)
	if err != nil {
		return hash, fmt.Errorf("unable to push the new config to the remote git repo: %w", err)
	}

	return hash, nil
}

func (s *gitStorage) Close() error {
	return nil
}

func (p *Panel) initGitIfNeeded(ctx context.Context) {
	logger.Debugf(ctx, "initGitIfNeeded")
	if p.data.GitRepo.Enable != nil && !*p.data.GitRepo.Enable {
		logger.Debugf(ctx, "git is disabled in the configuration")
		return
	}

	newUserData := false
	if p.data.GitRepo.URL == "" || p.data.GitRepo.PrivateKey == "" {
		newUserData = true
	}

	for {
		logger.Debugf(ctx, "attempting to configure a git storage")
		if newUserData {
			ok, err := p.inputGitUserData(ctx)
			if err != nil {
				p.displayError(fmt.Errorf("unable to input the git user data: %w", err))
				continue
			}
			if err := p.saveData(ctx); err != nil {
				p.displayError(err)
			}
			if !ok {
				p.deinitGitStorage(ctx)
				return
			}
		}

		logger.Debugf(ctx, "newGitStorage")
		gitStorage, err := newGitStorage(ctx, p.data.GitRepo.URL, []byte(p.data.GitRepo.PrivateKey))
		if err != nil {
			p.displayError(fmt.Errorf("unable to sync with the remote GIT repository: %w", err))
			if newUserData {
				continue
			}
			p.data.GitRepo.Enable = ptr(false)
			p.data.GitRepo.URL = ""
			p.data.GitRepo.PrivateKey = ""
			return
		}

		p.gitSyncerMutex.Lock()
		p.deinitGitStorage(ctx)
		if gitStorage == nil {
			panic("gitStorage == nil")
		}
		p.gitStorage = gitStorage
		p.gitSyncerMutex.Unlock()
		break
	}

	if p.gitStorage == nil {
		panic("p.gitStorage == nil")
	}
	p.startPeriodicGitSyncer(ctx)
	p.gitInitialized = true
}

func (p *Panel) deinitGitStorage(_ context.Context) {
	if p.gitStorage != nil {
		err := p.gitStorage.Close()
		if err != nil {
			p.displayError(err)
		}
		p.gitStorage = nil
	}
	if p.cancelGitSyncer != nil {
		p.cancelGitSyncer()
		p.cancelGitSyncer = nil
	}
}

func (p *Panel) startPeriodicGitSyncer(ctx context.Context) {
	logger.Debugf(ctx, "startPeriodicGitSyncer")
	defer logger.Debugf(ctx, "/startPeriodicGitSyncer")
	p.gitSyncerMutex.Lock()

	if p.cancelGitSyncer != nil {
		logger.Debugf(ctx, "git syncer is already started")
		p.gitSyncerMutex.Unlock()
		return
	}

	ctx, cancelFn := context.WithCancel(ctx)
	p.cancelGitSyncer = cancelFn
	p.gitSyncerMutex.Unlock()

	p.gitSync(ctx)
	go func() {
		err := p.sendConfigViaGIT(ctx)
		if err != nil {
			p.displayError(fmt.Errorf("unable to send the config to the remote git repository: %w", err))
		}

		ticker := time.NewTicker(time.Minute)
		for {
			select {
			case <-ctx.Done():
				p.gitSyncerMutex.Lock()
				defer p.gitSyncerMutex.Unlock()

				p.cancelGitSyncer = nil
				return
			case <-ticker.C:
			}

			p.gitSync(ctx)
		}
	}()
}

func (p *Panel) inputGitUserData(ctx context.Context) (bool, error) {
	logger.Debugf(ctx, "inputGitUserData")
	defer logger.Debugf(ctx, "/inputGitUserData")

	w := p.app.NewWindow("Configure GIT")
	w.Resize(fyne.NewSize(600, 600))

	waitCh := make(chan struct{})
	skip := false
	skipButton := widget.NewButtonWithIcon("Skip", theme.ConfirmIcon(), func() {
		skip = true
		close(waitCh)
	})
	okButton := widget.NewButtonWithIcon("OK", theme.ConfirmIcon(), func() {
		close(waitCh)
	})

	gitRepo := widget.NewEntry()
	gitRepo.SetText(p.data.GitRepo.URL)
	gitRepo.SetPlaceHolder("git@github.com:myname/myrepo.git")

	gitPrivateKey := widget.NewMultiLineEntry()
	gitPrivateKey.SetText(p.data.GitRepo.PrivateKey)
	gitPrivateKey.SetMinRowsVisible(10)
	gitPrivateKey.TextStyle.Monospace = true
	gitPrivateKey.SetPlaceHolder(`-----BEGIN OPENSSH PRIVATE KEY-----
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX
XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX=
-----END OPENSSH PRIVATE KEY-----`)

	gitInstruction := widget.NewRichTextFromMarkdown("We can sync the configuration among all of your devices via a git repository. To get a git repository you may, for example, use GitHub; but never use public repositories, always use private ones (because the repository will contain all the access credentials to YouTube/Twitch/whatever).")
	gitInstruction.Wrapping = fyne.TextWrapWord

	w.SetContent(container.NewBorder(
		nil,
		container.NewHBox(
			skipButton, okButton,
		),
		nil,
		nil,
		container.NewVBox(
			gitInstruction,
			widget.NewLabel("Remote repository:"),
			gitRepo,
			widget.NewLabel("Private key:"),
			gitPrivateKey,
		),
	))

	w.Show()
	<-waitCh
	w.Hide()

	if skip {
		p.data.GitRepo.Enable = ptr(false)
		return false, nil
	}

	p.data.GitRepo.Enable = ptr(true)
	p.data.GitRepo.URL = strings.Trim(gitRepo.Text, " \t\r\n")
	p.data.GitRepo.PrivateKey = strings.Trim(gitPrivateKey.Text, " \t\r\n")

	return true, nil
}

func (p *Panel) gitSync(ctx context.Context) {
	logger.Debugf(ctx, "gitSync")
	defer logger.Debugf(ctx, "/gitSync")

	p.gitSyncerMutex.Lock()
	defer p.gitSyncerMutex.Unlock()

	p.startStopMutex.Lock()

	if p.updateTimerHandler != nil {
		logger.Debugf(ctx, "skipping git sync: the stream is running")
		p.startStopMutex.Unlock()
		return
	}
	p.startStopMutex.Unlock()

	logger.Debugf(ctx, "last_known_commit: %s", p.data.GitRepo.LatestSyncCommit)
	err := p.gitStorage.Pull(
		ctx,
		p.getLastKnownGitCommitHash(),
		p.onConfigUpdateViaGIT,
	)
	if err != nil {
		p.displayError(fmt.Errorf("unable to sync with the remote GIT repository: %w", err))
	}
}

func (p *Panel) setLastKnownGitCommitHash(newHash plumbing.Hash) {
	p.data.GitRepo.LatestSyncCommit = newHash.String()
}

func (p *Panel) getLastKnownGitCommitHash() plumbing.Hash {
	return plumbing.NewHash(p.data.GitRepo.LatestSyncCommit)
}

func (p *Panel) onConfigUpdateViaGIT(ctx context.Context, newData *panelData) {
	p.data = *newData
	err := p.saveData(ctx)
	if err != nil {
		p.displayError(fmt.Errorf("unable to save data: %w", err))
	}
	if p.gitInitialized {
		p.needsRestart(ctx, "Received an updated config from another device, please restart the application")
	}
}

func (p *Panel) needsRestart(
	_ context.Context,
	msg string,
) {
	w := p.app.NewWindow("Needs restart: " + msg)
	w.Resize(fyne.NewSize(400, 300))
	textWidget := widget.NewMultiLineEntry()
	textWidget.SetText(msg)
	textWidget.Wrapping = fyne.TextWrapWord
	textWidget.TextStyle = fyne.TextStyle{
		Bold:      true,
		Monospace: true,
	}
	w.SetContent(textWidget)
	w.Show()
}

func (p *Panel) sendConfigViaGIT(
	ctx context.Context,
) error {
	logger.Debugf(ctx, "sendConfigViaGIT")
	defer logger.Debugf(ctx, "/sendConfigViaGIT")

	p.gitSyncerMutex.Lock()
	defer p.gitSyncerMutex.Unlock()

	logger.Debugf(ctx, "sendConfigViaGIT: Lock() success")

	worktree, err := p.gitStorage.Repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to open the git's worktree: %w", err)
	}

	refs, err := p.gitStorage.Repo.References()
	if err != nil {
		return fmt.Errorf("unable to get git references iterator: %w", err)
	}
	var ref *plumbing.Reference
	for {
		ref, err = refs.Next()
		if err != nil {
			return fmt.Errorf("unable to get the first git reference: %w", err)
		}
		if ref.Name() != "HEAD" {
			break
		}
	}

	if ref == nil {
		return fmt.Errorf("unable to find any branch references")
	}

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash:  ref.Hash(),
		Force: true,
	})
	if err != nil {
		return fmt.Errorf("unable to checkout git HEAD: %w", err)
	}

	f, err := worktree.Filesystem.Open(
		gitRepoPanelConfigFileName,
	)
	logger.Debugf(ctx, "file open result: %v", err)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to open file '%s' for reading: %w", gitRepoPanelConfigFileName, err)
	}

	var sha1SumBefore [sha1.Size]byte

	if f != nil {
		b, err := io.ReadAll(f)
		if err != nil {
			return fmt.Errorf("unable to read file '%s': %w", gitRepoPanelConfigFileName, err)
		}
		f.Close()
		sha1SumBefore = sha1.Sum(b)
	} else {
		logger.Debugf(ctx, "the file does not exist in the git repo, yet")
	}

	var newBytes bytes.Buffer
	latestSyncCommit := p.data.GitRepo.LatestSyncCommit
	p.data.GitRepo.LatestSyncCommit = ""
	err = writePanelData(ctx, &newBytes, p.data)
	p.data.GitRepo.LatestSyncCommit = latestSyncCommit
	if err != nil {
		return fmt.Errorf("unable to encode the config: %w", err)
	}

	sha1SumAfter := sha1.Sum(newBytes.Bytes())
	if bytes.Equal(sha1SumBefore[:], sha1SumAfter[:]) {
		logger.Debugf(ctx, "the config didn't change: %X == %X", sha1SumBefore[:], sha1SumAfter[:])
		return nil
	}
	logger.Debugf(ctx, "the config did change: %X == %X", sha1SumBefore[:], sha1SumAfter[:])

	f, err = worktree.Filesystem.OpenFile(
		gitRepoPanelConfigFileName,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		0644,
	)
	if err != nil {
		return fmt.Errorf("unable to open file '%s' for writing: %w", gitRepoPanelConfigFileName, err)
	}
	_, err = io.Copy(f, bytes.NewReader(newBytes.Bytes()))
	f.Close()
	if err != nil {
		return fmt.Errorf("unable to write the config into virtual git repo: %w", err)
	}

	hash, err := p.gitStorage.CommitAndPush(ctx, worktree, ref)
	if !hash.IsZero() {
		p.setLastKnownGitCommitHash(hash)
		if err := p.saveDataToConfigFile(ctx); err != nil {
			return fmt.Errorf("unable to store the new commit hash in the config file: %w", err)
		}
	}
	if errors.As(err, &ErrNeedsRebase{}) {
		p.gitSync(ctx)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to commit&push the config into virtual git repo: %w", err)
	}

	return nil
}
