package player

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	child_process_manager "github.com/AgustinSRG/go-child-process-manager"
	"github.com/DexterLB/mpvipc"
	"github.com/blang/mpv"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/xaionaro-go/streamctl/pkg/observability"
)

const SupportedMPV = true

const (
	TimeoutMPVStart = 10 * time.Second
)

const (
	restartMPV = true
)

var mpvCount uint64

type MPV struct {
	PlayerCommon
	PathToMPV  string
	SocketPath string
	Cmd        *exec.Cmd
	IPCClient  *mpv.IPCClient
	MPVClient  *mpv.Client
	MPVConn    *mpvipc.Connection

	EndChInitialized bool
	EndChMutex       sync.Mutex
	EndCh            chan struct{}

	OpenLinkOnRerun string
}

var _ Player = (*MPV)(nil)

func (m *Manager) NewMPV(
	ctx context.Context,
	title string,
) (*MPV, error) {
	r, err := NewMPV(ctx, title, m.Config.PathToMPV)
	if err != nil {
		return nil, err
	}

	logger.Tracef(ctx, "m.PlayersLocker.Lock()-ing")
	m.PlayersLocker.Lock()
	logger.Tracef(ctx, "m.PlayersLocker.Lock()-ed")
	defer logger.Tracef(ctx, "m.PlayersLocker.Unlock()-ed")
	defer m.PlayersLocker.Unlock()
	m.Players = append(m.Players, r)
	return r, nil
}

func NewMPV(
	ctx context.Context,
	title string,
	pathToMPV string,
) (_ret *MPV, _err error) {
	if pathToMPV == "" {
		pathToMPV = "mpv"
		switch runtime.GOOS {
		case "windows":
			pathToMPV += ".exe"
		}
	}
	p := &MPV{
		PlayerCommon: PlayerCommon{
			Title: title,
		},
		PathToMPV: pathToMPV,
		EndCh:     make(chan struct{}),
	}
	err := p.execMPV(ctx)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *MPV) execMPV(ctx context.Context) (_ret error) {
	myPid := os.Getpid()
	mpvID := atomic.AddUint64(&mpvCount, 1)
	var socketPath string
	switch runtime.GOOS {
	case "windows":
		socketPath = `\\.\pipe\` + fmt.Sprintf("mpv-ipc-%d-%d", myPid, mpvID)
	default:
		tempDir := os.TempDir()
		socketPath = path.Join(tempDir, fmt.Sprintf("mpv-ipc-%d-%d.sock", myPid, mpvID))
	}
	_ = os.Remove(socketPath)

	logger.Debugf(ctx, "socket path: '%s'", socketPath)

	args := []string{p.PathToMPV, "--idle", "--keep-open=always", "--keep-open-pause=no", "--input-ipc-server=" + socketPath, fmt.Sprintf("--title=%s", p.Title)}
	logger.Debugf(ctx, "running command '%s %s'", args[0], strings.Join(args[1:], " "))
	cmd := exec.Command(args[0], args[1:]...)
	if observability.LogLevelFilter.GetLevel() >= logger.LevelTrace {
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
	}
	err := child_process_manager.ConfigureCommand(cmd)
	errmon.ObserveErrorCtx(ctx, err)
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("unable to start mpv: %w", err)
	}
	err = child_process_manager.AddChildProcess(cmd.Process)
	errmon.ObserveErrorCtx(ctx, err)
	logger.Debugf(ctx, "started command '%s %s'", args[0], strings.Join(args[1:], " "))

	logger.Debugf(ctx, "waiting for the socket '%s' to get ready", socketPath)

	mpvConn := mpvipc.NewConnection(socketPath)
	t := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		if err := mpvConn.Open(); err == nil {
			break
		}
	}
	logger.Debugf(ctx, "socket '%s' is ready", socketPath)
	p.SocketPath = socketPath
	p.Cmd = cmd
	p.MPVConn = mpvConn

	if restartMPV {
		observability.Go(ctx, func() {
			err := p.Cmd.Wait()
			logger.Debugf(ctx, "player was closed: %v", err)
			link := p.OpenLinkOnRerun
			err = p.Close(ctx)
			logger.Debugf(ctx, "cleanup result: %v", err)
			select {
			case <-ctx.Done():
				logger.Debugf(ctx, "context is closed, not rerunning the player")
				return
			default:
			}
			logger.Debugf(ctx, "rerunning the player")
			err = p.execMPV(ctx)
			if err != nil {
				logger.Error(ctx, "unable to rerun the player: %v", err)
			}
			logger.Debugf(ctx, "successfully reran the player")
			if link != "" {
				logger.Debugf(ctx, "reopen link '%s'")
				err := p.OpenURL(ctx, link)
				if err != nil {
					logger.Errorf(ctx, "unable to reopen link '%v'", err)
				}
			}
		})
	}
	return nil
}

func (p *MPV) OpenURL(
	ctx context.Context,
	link string,
) error {
	p.OpenLinkOnRerun = link
	_, err := p.MPVConn.Call("loadfile", link, "replace")
	return err
}

func (p *MPV) getString(key string) (string, error) {
	r, err := p.MPVConn.Get(key)
	if err != nil {
		return "", fmt.Errorf("unable to get '%s' from the MPV: %w", key, err)
	}
	s, ok := r.(string)
	if !ok {
		s = fmt.Sprint(r)
	}
	return s, nil
}

func (p *MPV) getFloat64(key string) (float64, error) {
	r, err := p.MPVConn.Get(key)
	if err != nil {
		return 0, fmt.Errorf("unable to get '%s' from the MPV: %w", key, err)
	}
	switch r := r.(type) {
	case float64:
		return r, nil
	case string:
		return strconv.ParseFloat(r, 64)
	default:
		return 0, fmt.Errorf("unexpected type %T", r)
	}
}

func (p *MPV) GetLink(
	ctx context.Context,
) (string, error) {
	return p.getString("filename")
}

func (p *MPV) EndChan(
	ctx context.Context,
) (<-chan struct{}, error) {
	p.EndChMutex.Lock()
	defer p.EndChMutex.Unlock()
	p.initEndCh(ctx)
	return p.EndCh, nil
}

func (p *MPV) initEndCh(
	ctx context.Context,
) {
	if p.EndChInitialized {
		return
	}
	observability.Go(ctx, func() {
		func() {
			t := time.NewTimer(time.Millisecond * 100)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
				isEnded, _ := p.IsEnded(ctx)
				if !isEnded {
					return
				}
			}
		}()
		p.EndChMutex.Lock()
		defer p.EndChMutex.Unlock()
		var oldCh chan struct{}
		oldCh, p.EndCh = p.EndCh, make(chan struct{})
		close(oldCh)
		p.EndChInitialized = false
	})
}

func (p *MPV) IsEnded(
	ctx context.Context,
) (bool, error) {
	link, err := p.GetLink(ctx)
	if err != nil {
		return false, nil
	}
	return link != "", nil
}

func (p *MPV) GetPosition(
	ctx context.Context,
) (time.Duration, error) {
	ts, err := p.getFloat64("time-pos")
	if err != nil {
		return 0, err
	}

	return time.Duration(ts * float64(time.Second)), nil
}

func (p *MPV) GetLength(
	ctx context.Context,
) (time.Duration, error) {
	ts, err := p.getFloat64("duration")
	if err != nil {
		return 0, err
	}

	return time.Duration(ts * float64(time.Second)), nil
}

func (p *MPV) SetSpeed(
	ctx context.Context,
	speed float64,
) error {
	return p.MPVConn.Set("speed", speed)
}

func (p *MPV) GetSpeed(
	ctx context.Context,
) (float64, error) {
	return p.getFloat64("speed")
}

func (p *MPV) SetPause(
	ctx context.Context,
	pause bool,
) error {
	return p.MPVConn.Set("pause", pause)
}

func (p *MPV) Stop(
	ctx context.Context,
) error {
	_, err := p.MPVConn.Call("stop")
	if err != nil {
		return fmt.Errorf("unable to request 'stop'-ing: %w", err)
	}
	return nil
}

func (p *MPV) Close(ctx context.Context) error {
	p.OpenLinkOnRerun = ""
	return multierror.Append(
		p.Cmd.Process.Kill(),
		os.Remove(p.SocketPath),
	).ErrorOrNil()
}
