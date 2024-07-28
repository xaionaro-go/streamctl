package player

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blang/mpv"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
)

const (
	TimeoutMPVStart = 10 * time.Second
)

var mpvCount uint64

type MPV struct {
	PlayerCommon
	SocketPath string
	Cmd        *exec.Cmd
	IPCClient  *mpv.IPCClient
	MPVClient  *mpv.Client

	EndChInitialized bool
	EndChMutex       sync.Mutex
	EndCh            chan struct{}
}

var _ Player = (*MPV)(nil)

func NewMPV(title string, pathToMPV string) (*MPV, error) {
	if pathToMPV == "" {
		pathToMPV = "mpv"
		switch runtime.GOOS {
		case "windows":
			pathToMPV += ".exe"
		}
	}

	myPid := os.Getpid()
	mpvID := atomic.AddUint64(&mpvCount, 1)
	tempDir := os.TempDir()
	socketPath := path.Join(tempDir, fmt.Sprintf("mpv-ipc-%d-%d.sock", myPid, mpvID))
	_ = os.Remove(socketPath)

	cmd := exec.Command(pathToMPV, "--idle", "--input-ipc-server="+socketPath, fmt.Sprintf("--title=%s", title))
	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("unable to start mpv: %w", err)
	}

	t := time.NewTicker(100 * time.Millisecond)
	for {
		<-t.C
		if _, err := os.Stat(socketPath); errors.Is(err, os.ErrNotExist) {
			continue
		}
		break
	}

	ipcc := mpv.NewIPCClient(socketPath)
	mpvc := mpv.NewClient(ipcc)
	return &MPV{
		PlayerCommon: PlayerCommon{
			Title: title,
		},
		SocketPath: socketPath,
		Cmd:        cmd,
		IPCClient:  ipcc,
		MPVClient:  mpvc,
		EndCh:      make(chan struct{}),
	}, nil
}

func (p *MPV) OpenURL(link string) error {
	return p.MPVClient.Loadfile(link, mpv.LoadFileModeReplace)
}

func (p *MPV) GetLink() string {
	r, _ := p.MPVClient.Filename()
	return r
}

func (p *MPV) EndChan() <-chan struct{} {
	p.EndChMutex.Lock()
	defer p.EndChMutex.Unlock()
	p.initEndCh()
	return p.EndCh
}

func (p *MPV) initEndCh() {
	if p.EndChInitialized {
		return
	}
	t := time.NewTimer(time.Millisecond * 100)
	defer t.Stop()
	for {
		<-t.C
		if !p.IsEnded() {
			break
		}
	}
	p.EndChMutex.Lock()
	defer p.EndChMutex.Unlock()
	var oldCh chan struct{}
	oldCh, p.EndCh = p.EndCh, make(chan struct{})
	close(oldCh)
}

func (p *MPV) IsEnded() bool {
	filename, err := p.MPVClient.Filename()
	if err != nil {
		logger.Tracef(context.TODO(), "unable to get the filename: %v", err)
	}
	return filename != ""
}

func (p *MPV) GetPosition() time.Duration {
	ts, err := p.MPVClient.Position()
	if err != nil {
		logger.Tracef(context.TODO(), "unable to get current position: %v", err)
		return 0
	}

	return time.Duration(ts * float64(time.Second))
}

func (p *MPV) GetLength() time.Duration {
	ts, err := p.MPVClient.Duration()
	if err != nil {
		logger.Debugf(context.TODO(), "unable to get the total length: %v", err)
		return 0
	}

	return time.Duration(ts * float64(time.Second))
}

func (p *MPV) SetSpeed(speed float64) error {
	return p.MPVClient.SetProperty("speed", speed)
}

func (p *MPV) GetSpeed() (float64, error) {
	return p.MPVClient.Speed()
}

func (p *MPV) SetPause(pause bool) error {
	return p.MPVClient.SetPause(pause)
}

func (p *MPV) Stop() error {
	resp, err := p.MPVClient.Exec("stop")
	if err != nil {
		return fmt.Errorf("unable to request 'stop'-ing: %w", err)
	}
	if resp.Err != "" {
		return fmt.Errorf("'stop'-ing failed: %s", resp.Err)
	}
	return nil
}

func (p *MPV) Close() error {
	return multierror.Append(
		p.Cmd.Process.Kill(),
		os.Remove(p.SocketPath),
	).ErrorOrNil()
}
