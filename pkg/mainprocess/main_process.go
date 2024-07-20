package mainprocess

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"sync"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/hashicorp/go-multierror"
	"github.com/sethvargo/go-password/password"
)

type OnReceivedMessageFunc func(
	ctx context.Context,
	source string,
	content any,
) error

type Manager struct {
	listener net.Listener
	password string

	connsLocker  sync.Mutex
	conns        map[string]net.Conn
	connsChanged chan struct{}

	allClientProcesses []string

	OnReceivedMessage OnReceivedMessageFunc
}

func NewManager(
	onReceivedMessage OnReceivedMessageFunc,
	expectedClients ...string,
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
		OnReceivedMessage: onReceivedMessage,

		listener: listener,
		password: password,

		conns:        map[string]net.Conn{},
		connsChanged: make(chan struct{}),

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

func (m *Manager) Serve(ctx context.Context) error {
	logger.Tracef(ctx, "serving listener at %s", m.listener.Addr())
	defer logger.Tracef(ctx, "/serving listener at %s", m.listener.Addr())

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	go func() {
		<-ctx.Done()
		err := m.Close()
		if err != nil {
			logger.Error(ctx, err)
		}
	}()

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

		m.addNewConnection(ctx, conn)
	}
}

func (m *Manager) addNewConnection(
	ctx context.Context,
	conn net.Conn,
) {
	go func() {
		m.handleConnection(ctx, conn)
	}()
}

func (m *Manager) handleConnection(
	ctx context.Context,
	conn net.Conn,
) {
	var regMessage RegistrationMessage
	logger.Tracef(ctx, "handleConnection from %s", conn.RemoteAddr())
	defer func() { logger.Tracef(ctx, "/handleConnection from %s (%s)", conn.RemoteAddr(), regMessage.Source) }()

	ctx, cancelFn := context.WithCancel(ctx)
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
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
	if err := m.registerConnection(regMessage.Source, conn); err != nil {
		err = fmt.Errorf("unable to register process '%s': %w", regMessage.Source, err)
		encoder.Encode(RegistrationResult{Error: err.Error()})
		logger.Error(ctx, err)
		return
	}
	defer func(sourceName string) {
		m.unregisterConnection(sourceName)
	}(regMessage.Source)
	if err := encoder.Encode(RegistrationResult{}); err != nil {
		err = fmt.Errorf("unable to encode&send the registration result to '%s': %w", regMessage.Source, err)
		logger.Error(ctx, err)
		return
	}
	ctx = belt.WithField(ctx, "client", regMessage.Source)

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
		logger.Tracef(ctx, "getting a message from '%s': %#+v %#+v", regMessage.Source, message, err)
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

		if err := m.processMessage(ctx, regMessage.Source, message); err != nil {
			logger.Errorf(
				ctx,
				"unable to process the message %#+v from %s (%s): %w",
				message, regMessage.Source, conn.RemoteAddr().String(), err,
			)
		}
		logger.Tracef(ctx, "next iteration")
	}
}

func (m *Manager) processMessage(
	ctx context.Context,
	source string,
	message MessageToMain,
) (_ret error) {
	logger.Tracef(ctx, "processing message from '%s': %#+v", source, message)
	defer func() { logger.Tracef(ctx, "/processing message from '%s': %#+v: %v", source, message, _ret) }()

	switch message.Destination {
	case "":
		logger.Tracef(ctx, "a broadcast message from '%s': %#+v", source, message.Content)
		var wg sync.WaitGroup
		var err *multierror.Error
		err = multierror.Append(err, m.onReceivedMessage(ctx, source, message.Content))

		errCh := make(chan error)
		go func() {
			for e := range errCh {
				err = multierror.Append(err, e)
			}
		}()
		for _, dst := range m.allClientProcesses {
			if dst == source {
				continue
			}
			wg.Add(1)
			go func(dst string) {
				defer wg.Done()
				errCh <- m.sendMessage(ctx, source, dst, message.Content)
			}(dst)
		}
		wg.Wait()
		close(errCh)
		return err.ErrorOrNil()
	case "main":
		logger.Tracef(ctx, "a message to the main process from '%s': %#+v", source, message.Content)
		return m.onReceivedMessage(ctx, source, message.Content)
	default:
		logger.Tracef(ctx, "a message to '%s' from '%s': %#+v", message.Destination, source, message.Content)
		return m.sendMessage(ctx, source, message.Destination, message.Content)
	}
}

func (m *Manager) onReceivedMessage(
	ctx context.Context,
	source string,
	content any,
) error {
	if m.OnReceivedMessage == nil {
		err := fmt.Errorf("OnReceivedMessage is not set")
		logger.Tracef(ctx, "%v", err)
		return err
	}

	logger.Tracef(ctx, "calling the OnReceivedMessage function")
	return m.OnReceivedMessage(ctx, source, content)
}

type MessageFromMain struct {
	Source      string
	Password    string
	Destination string
	Content     any
}

func (m *Manager) isExpectedProcess(
	name string,
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
	source string,
	destination string,
	content any,
) (_ret error) {
	logger.Tracef(ctx, "sending message message %#+v from '%s' to '%s'", content, source, destination)
	defer func() {
		logger.Tracef(ctx, "/sending message message %#+v from '%s' to '%s': %v", content, source, destination, _ret)
	}()

	if !m.isExpectedProcess(destination) {
		return fmt.Errorf("process '%s' is not ever expected", destination)
	}

	message := MessageFromMain{
		Source:      source,
		Password:    m.password,
		Destination: destination,
		Content:     content,
	}

	conn, err := m.waitForProcess(destination)
	if err != nil {
		return fmt.Errorf("unable to wait for process '%s': %w", destination, err)
	}

	encoder := gob.NewEncoder(conn)
	err = encoder.Encode(message)
	if err != nil {
		return fmt.Errorf("unable to encode&send message: %w", err)
	}

	return nil
}

func (m *Manager) waitForProcess(
	name string,
) (net.Conn, error) {
	if !m.isExpectedProcess(name) {
		return nil, fmt.Errorf("process '%s' is not ever expected", name)
	}

	for {
		m.connsLocker.Lock()
		conn := m.conns[name]
		ch := m.connsChanged
		m.connsLocker.Unlock()

		if conn != nil {
			return conn, nil
		}

		<-ch
	}
}

func (m *Manager) checkPassword(
	password string,
) error {
	return checkPassword(m.password, password)
}

func (m *Manager) registerConnection(
	sourceName string,
	conn net.Conn,
) error {
	if !m.isExpectedProcess(sourceName) {
		return fmt.Errorf("process '%s' is not ever expected", sourceName)
	}

	m.connsLocker.Lock()
	defer m.connsLocker.Unlock()
	if conn, ok := m.conns[sourceName]; ok {
		return fmt.Errorf("process '%s' is already registered at %s", sourceName, conn.RemoteAddr().String())
	}
	m.conns[sourceName] = conn
	var oldCh chan struct{}
	oldCh, m.connsChanged = m.connsChanged, make(chan struct{})
	close(oldCh)
	return nil
}

func (m *Manager) unregisterConnection(
	sourceName string,
) {
	m.connsLocker.Lock()
	defer m.connsLocker.Unlock()
	delete(m.conns, sourceName)
	var oldCh chan struct{}
	oldCh, m.connsChanged = m.connsChanged, make(chan struct{})
	close(oldCh)
}

type RegistrationMessage struct {
	Password string
	Source   string
}

type RegistrationResult struct {
	Error string
}

type MessageToMain struct {
	Password    string
	Destination string
	Content     any
}
