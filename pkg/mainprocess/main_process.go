package mainprocess

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/sethvargo/go-password/password"
	"github.com/xaionaro-go/lockmap"
	"github.com/xaionaro-go/streamctl/pkg/observability"
	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

func init() {
	gob.Register(RegistrationMessage{})
	gob.Register(RegistrationResult{})
	gob.Register(MessageReady{})
	gob.Register(MessageReadyConfirmed{})
	gob.Register(MessageFromMain{})
	gob.Register(MessageToMain{})
}

type OnReceivedMessageFunc func(
	ctx context.Context,
	source ProcessName,
	content any,
) error

type LaunchClientFunc func(
	ctx context.Context,
	procName ProcessName,
	addr string,
	password string,
	isRestart bool,
) error

type Manager struct {
	listener net.Listener
	password string

	connsLocker   xsync.Mutex
	conns         map[ProcessName]net.Conn
	connLocker    *lockmap.LockMap
	childReadyFor map[ProcessName]map[reflect.Type]struct{}
	connsChanged  chan struct{}

	allClientProcesses []ProcessName

	LaunchClient LaunchClientFunc
}

func NewManager(
	launchClient LaunchClientFunc,
	expectedClients ...ProcessName,
) (*Manager, error) {
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	password, err := password.Generate(16, 4, 4, false, true)
	if err != nil {
		return nil, fmt.Errorf("unable to generate a password: %w", err)
	}

	return &Manager{
		LaunchClient: launchClient,

		listener: listener,
		password: password,

		conns:         map[ProcessName]net.Conn{},
		connLocker:    lockmap.NewLockMap(),
		childReadyFor: map[ProcessName]map[reflect.Type]struct{}{},
		connsChanged:  make(chan struct{}),

		allClientProcesses: expectedClients,
	}, nil
}

func (m *Manager) Password() string {
	return m.password
}

func (m *Manager) Addr() net.Addr {
	return m.listener.Addr()
}

func (m *Manager) Close() error {
	return m.listener.Close()
}

func (m *Manager) VerifyEverybodyConnected(
	ctx context.Context,
) error {
	return xsync.DoR1(ctx, &m.connsLocker, func() error {
		for _, name := range m.allClientProcesses {
			if _, ok := m.conns[name]; !ok {
				return fmt.Errorf("client '%s' is not connected", name)
			}
		}
		return nil
	})
}

func (m *Manager) Serve(
	ctx context.Context,
	onReceivedMessage OnReceivedMessageFunc,
) error {
	logger.Tracef(ctx, "serving listener at %s", m.listener.Addr())
	defer logger.Tracef(ctx, "/serving listener at %s", m.listener.Addr())

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	observability.Go(ctx, func() {
		<-ctx.Done()
		err := m.Close()
		if err != nil {
			logger.Debug(ctx, err)
		}
	})

	if m.LaunchClient != nil {
		for _, name := range m.allClientProcesses {
			err := m.LaunchClient(ctx, name, m.listener.Addr().String(), m.password, false)
			if err != nil {
				return fmt.Errorf("unable to launch '%s': %w", name, err)
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		conn, err := m.listener.Accept()
		if err != nil {
			return fmt.Errorf("unable to accept connection: %w", err)
		}
		logger.Tracef(ctx, "accepted a connection from '%s'", conn.RemoteAddr())
		conn.(*net.TCPConn).SetNoDelay(true)

		m.addNewConnection(ctx, conn, onReceivedMessage)
	}
}

func (m *Manager) addNewConnection(
	ctx context.Context,
	conn net.Conn,
	onReceivedMessage OnReceivedMessageFunc,
) {
	observability.Go(ctx, func() {
		m.handleConnection(ctx, conn, onReceivedMessage)
	})
}

func (m *Manager) handleConnection(
	ctx context.Context,
	conn net.Conn,
	onReceivedMessage OnReceivedMessageFunc,
) {
	var regMessage RegistrationMessage
	logger.Tracef(ctx, "handleConnection from %s", conn.RemoteAddr())
	defer func() { logger.Tracef(ctx, "/handleConnection from %s (%s)", conn.RemoteAddr(), regMessage.Source) }()

	ctx, cancelFn := context.WithCancel(ctx)
	observability.Go(ctx, func() {
		<-ctx.Done()
		conn.Close()
	})
	defer cancelFn()

	encoder := gob.NewEncoder(conn)

	decoder := gob.NewDecoder(conn)
	err := decoder.Decode(&regMessage)
	if err != nil {
		err = fmt.Errorf("unable to decode registration message: %w", err)
		encoder.Encode(RegistrationResult{Error: err.Error()})
		logger.Debug(ctx, err)
		return
	}

	logger.Debugf(ctx, "received registration message: %#+v", regMessage)
	if err := m.checkPassword(regMessage.Password); err != nil {
		regMessage = RegistrationMessage{}
		err = fmt.Errorf("invalid password: %w", err)
		encoder.Encode(RegistrationResult{Error: err.Error()})
		logger.Warn(ctx, err)
		return
	}
	if err := m.registerConnection(ctx, regMessage.Source, conn); err != nil {
		err = fmt.Errorf("unable to register process '%s': %w", regMessage.Source, err)
		encoder.Encode(RegistrationResult{Error: err.Error()})
		logger.Error(ctx, err)
		return
	}
	defer func(sourceName ProcessName) {
		m.unregisterConnection(ctx, sourceName)
	}(regMessage.Source)
	if err := encoder.Encode(RegistrationResult{}); err != nil {
		err = fmt.Errorf(
			"unable to encode&send the registration result to '%s': %w",
			regMessage.Source,
			err,
		)
		logger.Error(ctx, err)
		return
	}
	ctx = belt.WithField(ctx, "client", regMessage.Source)

	defer func() {
		if m.LaunchClient == nil {
			return
		}
		err := m.LaunchClient(ctx, regMessage.Source, m.listener.Addr().String(), m.password, true)
		if err != nil {
			logger.Error(ctx, err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			logger.Tracef(ctx, "context was closed")
			return
		default:
		}
		var message MessageToMain
		logger.Tracef(ctx, "waiting for a message from '%s'", regMessage.Source)
		decoder := gob.NewDecoder(conn)
		err := decoder.Decode(&message)
		logger.Tracef(
			ctx,
			"getting a message from '%s': %#+v %#+v",
			regMessage.Source,
			message,
			err,
		)
		select {
		case <-ctx.Done():
			logger.Tracef(ctx, "context was closed")
			return
		default:
		}
		if err != nil {
			err = fmt.Errorf(
				"unable to parse the message from %s (%s): %w",
				regMessage.Source,
				conn.RemoteAddr().String(),
				err,
			)
			logger.Error(ctx, err)
			return
		}

		if err := m.processMessage(ctx, regMessage.Source, message, onReceivedMessage); err != nil {
			logger.Errorf(ctx, "unable to process the message %#+v from %s (%s): %v",
				message, regMessage.Source, conn.RemoteAddr().String(), err,
			)
		}
		logger.Tracef(ctx, "next iteration")
	}
}

func (m *Manager) processMessage(
	ctx context.Context,
	source ProcessName,
	message MessageToMain,
	onReceivedMessage OnReceivedMessageFunc,
) (_ret error) {
	logger.Tracef(ctx, "processing message from '%s': %#+v", source, message)
	defer func() { logger.Tracef(ctx, "/processing message from '%s': %#+v: %v", source, message, _ret) }()

	switch message.Destination {
	case "":
		logger.Tracef(ctx, "a broadcast message from '%s': %#+v", source, message.Content)
		var wg sync.WaitGroup
		var err *multierror.Error
		err = multierror.Append(err, onReceivedMessage(ctx, source, message.Content))

		errCh := make(chan error)
		observability.Go(ctx, func() {
			for e := range errCh {
				err = multierror.Append(err, e)
			}
		})
		for _, dst := range m.allClientProcesses {
			if dst == source {
				continue
			}
			wg.Add(1)
			{
				dst := dst
				observability.Go(ctx, func() {
					defer wg.Done()
					errCh <- m.sendMessage(ctx, source, dst, message.Content)
				})
			}
		}
		wg.Wait()
		close(errCh)
		return err.ErrorOrNil()
	case ProcessNameMain:
		logger.Tracef(
			ctx,
			"got a message to the main process from '%s': %#+v",
			source,
			message.Content,
		)
		switch content := message.Content.(type) {
		case MessageReady:
			var result *multierror.Error
			result = multierror.Append(result, m.SendMessagePreReady(ctx, source, MessageReadyConfirmed{}))
			for _, sample := range content.ReadyForMessages {
				result = multierror.Append(result, m.setReady(ctx, source, reflect.TypeOf(sample)))
			}
			return result.ErrorOrNil()
		default:
			return onReceivedMessage(ctx, source, message.Content)
		}
	default:
		logger.Tracef(
			ctx,
			"got a message to '%s' from '%s': %#+v",
			message.Destination,
			source,
			message.Content,
		)
		return m.sendMessage(ctx, source, message.Destination, message.Content)
	}
}

type MessageFromMain struct {
	Source      ProcessName
	Password    string
	Destination ProcessName
	Content     any
}

func (m *Manager) isExpectedProcess(
	name ProcessName,
) bool {
	for _, p := range m.allClientProcesses {
		if name == p {
			return true
		}
	}
	return false
}

func (m *Manager) sendMessage(
	ctx context.Context,
	source ProcessName,
	destination ProcessName,
	content any,
) (_ret error) {
	logger.Tracef(ctx, "sending message %#+v from '%s' to '%s'", content, source, destination)
	defer func() {
		logger.Tracef(
			ctx,
			"/sending message %#+v from '%s' to '%s': %v",
			content,
			source,
			destination,
			_ret,
		)
	}()

	if !m.isExpectedProcess(destination) {
		return fmt.Errorf("process '%s' is not ever expected", destination)
	}

	observability.Go(ctx, func() {
		conn, err := m.waitForReadyProcess(ctx, destination, reflect.TypeOf(content))
		if err != nil {
			logger.Errorf(
				ctx,
				"%v",
				fmt.Errorf("unable to wait for process '%s': %w", destination, err),
			)
			return
		}

		message := MessageFromMain{
			Source:      source,
			Password:    m.password,
			Destination: destination,
			Content:     content,
		}

		h := m.connLocker.Lock(context.Background(), destination)
		defer h.Unlock()
		defer time.Sleep(
			100 * time.Millisecond,
		) // TODO: Delete this horrible hack (that is introduced to avoid erasing messages in the buffer)
		err = gob.NewEncoder(conn).Encode(message)
		if err != nil {
			logger.Errorf(ctx, "%v", fmt.Errorf("unable to encode&send message: %w", err))
			return
		}
	})

	return nil
}

func (m *Manager) waitForProcess(
	ctx context.Context,
	name ProcessName,
) (net.Conn, error) {
	if !m.isExpectedProcess(name) {
		return nil, fmt.Errorf("process '%s' is not ever expected", name)
	}

	for {
		conn, ch := xsync.DoR2(ctx, &m.connsLocker, func() (net.Conn, chan struct{}) {
			return m.conns[name], m.connsChanged
		})

		if conn != nil {
			return conn, nil
		}

		<-ch
	}
}

func (m *Manager) waitForReadyProcess(
	ctx context.Context,
	name ProcessName,
	msgType reflect.Type,
) (net.Conn, error) {
	logger.Debugf(ctx, "waitForReadyProcess(ctx, '%s', '%s')", name, msgType)
	defer logger.Debugf(ctx, "/waitForReadyProcess(ctx, '%s', '%s')", name, msgType)
	if !m.isExpectedProcess(name) {
		return nil, fmt.Errorf("process '%s' is not ever expected", name)
	}
	if msgType.Kind() == reflect.Pointer {
		msgType = msgType.Elem()
	}

	for {
		conn, ch, isReady := xsync.DoR3(
			ctx,
			&m.connsLocker,
			func() (net.Conn, chan struct{}, bool) {
				readyMap := m.childReadyFor[name]
				isReady := false
				if readyMap != nil {
					if _, ok := readyMap[msgType]; ok {
						isReady = true
					}
				}
				return m.conns[name], m.connsChanged, isReady
			},
		)

		if conn != nil && isReady {
			logger.Debugf(
				ctx,
				"waitForReadyProcess(ctx, '%s', '%s'): waiting is complete",
				name,
				msgType,
			)
			return conn, nil
		}

		logger.Debugf(
			ctx,
			"waitForReadyProcess(ctx, '%s', '%s'): waiting for a change in connections",
			name,
			msgType,
		)
		<-ch
	}
}

func (m *Manager) checkPassword(
	password string,
) error {
	return checkPassword(m.password, password)
}

func (m *Manager) registerConnection(
	ctx context.Context,
	sourceName ProcessName,
	conn net.Conn,
) error {
	logger.Debugf(ctx, "registerConnection(ctx, '%s', %s)", sourceName, conn.RemoteAddr().String())
	defer logger.Debugf(
		ctx,
		"/registerConnection(ctx, '%s', %s)",
		sourceName,
		conn.RemoteAddr().String(),
	)
	if !m.isExpectedProcess(sourceName) {
		return fmt.Errorf("process '%s' is not ever expected", sourceName)
	}

	m.connsLocker.Do(ctx, func() {
		if conn, ok := m.conns[sourceName]; ok {
			conn.Close()
			logger.Warnf(ctx, "closing the old registered connection")
		}
		m.conns[sourceName] = conn
		var oldCh chan struct{}
		oldCh, m.connsChanged = m.connsChanged, make(chan struct{})
		close(oldCh)
	})
	return nil
}

func (m *Manager) setReady(
	ctx context.Context,
	sourceName ProcessName,
	msgType reflect.Type,
) error {
	if !m.isExpectedProcess(sourceName) {
		return fmt.Errorf("process '%s' is not ever expected", sourceName)
	}
	if msgType.Kind() == reflect.Pointer {
		msgType = msgType.Elem()
	}

	return xsync.DoR1(ctx, &m.connsLocker, func() error {
		readyMap := m.childReadyFor[sourceName]
		if readyMap == nil {
			m.childReadyFor[sourceName] = map[reflect.Type]struct{}{}
			readyMap = m.childReadyFor[sourceName]
		}
		if _, ok := readyMap[msgType]; ok {
			return fmt.Errorf("process '%s' is already ready for '%s'", sourceName, msgType)
		}
		readyMap[msgType] = struct{}{}
		var oldCh chan struct{}
		oldCh, m.connsChanged = m.connsChanged, make(chan struct{})
		close(oldCh)
		return nil
	})
}

func (m *Manager) unregisterConnection(
	ctx context.Context,
	sourceName ProcessName,
) {
	logger.Infof(ctx, "unregistering process '%s'", string(sourceName))
	defer logger.Infof(ctx, "/unregistering process '%s'", string(sourceName))
	m.connsLocker.Do(ctx, func() {
		delete(m.conns, sourceName)
		delete(m.childReadyFor, sourceName)
		var oldCh chan struct{}
		oldCh, m.connsChanged = m.connsChanged, make(chan struct{})
		close(oldCh)
	})
}

type RegistrationMessage struct {
	Password string
	Source   ProcessName
}

type RegistrationResult struct {
	Error string
}

type MessageToMain struct {
	Password    string
	Destination ProcessName
	Content     any
}

func (m *Manager) SendMessagePreReady(
	ctx context.Context,
	dst ProcessName,
	content any,
) error {
	logger.Debugf(ctx, "SendMessagePreReady(ctx, '%s', %T)", dst, content)
	defer logger.Debugf(ctx, "/SendMessagePreReady(ctx, '%s', %T)", dst, content)

	conn, err := m.waitForProcess(ctx, dst)
	if err != nil {
		return fmt.Errorf("unable to wait for process '%s': %w", dst, err)
	}
	encoder := gob.NewEncoder(conn)
	msg := MessageFromMain{
		Source:      ProcessNameMain,
		Password:    m.password,
		Destination: dst,
		Content:     content,
	}
	h := m.connLocker.Lock(context.Background(), dst)
	defer h.Unlock()
	defer time.Sleep(
		100 * time.Millisecond,
	) // TODO: Delete this horrible hack (that is introduced to avoid erasing messages in the buffer)
	err = encoder.Encode(msg)
	logger.Tracef(ctx, "sending message %#+v: %v", msg, err)
	if err != nil {
		return fmt.Errorf("unable to encode&send message %#+v: %w", msg, err)
	}
	return nil
}

func (m *Manager) SendMessage(
	ctx context.Context,
	dst ProcessName,
	content any,
) error {
	logger.Debugf(ctx, "SendMessage(ctx, '%s', %T)", dst, content)
	defer logger.Debugf(ctx, "/SendMessage(ctx, '%s', %T)", dst, content)

	conn, err := m.waitForReadyProcess(ctx, dst, reflect.TypeOf(content))
	if err != nil {
		return fmt.Errorf("unable to wait for process '%s': %w", dst, err)
	}
	encoder := gob.NewEncoder(conn)
	msg := MessageFromMain{
		Source:      ProcessNameMain,
		Password:    m.password,
		Destination: dst,
		Content:     content,
	}
	h := m.connLocker.Lock(context.Background(), dst)
	defer h.Unlock()
	err = encoder.Encode(msg)
	logger.Tracef(ctx, "sending message %#+v: %v", msg, err)
	if err != nil {
		return fmt.Errorf("unable to encode&send message %#+v: %w", msg, err)
	}
	return nil
}

type MessageReady struct {
	ReadyForMessages []any
}
type MessageReadyConfirmed struct{}
