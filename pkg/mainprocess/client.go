package mainprocess

import (
	"context"
	"encoding/gob"
	"fmt"
	"net"
	"sync"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type Client struct {
	WriteLocker sync.Mutex
	Conn        net.Conn
	Password    string
}

func NewClient(
	myName ProcessName,
	addr string,
	password string,
) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to '%s': %w", addr, err)
	}
	logger.Default().Tracef("connected to '%s' from '%s'", conn.RemoteAddr(), conn.LocalAddr())
	conn.(*net.TCPConn).SetNoDelay(true)

	msg := RegistrationMessage{
		Password: password,
		Source:   myName,
	}
	encoder := gob.NewEncoder(conn)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("unable to encode&send the registration message %#+v: %w", msg, err)
	}

	var regResult RegistrationResult
	decoder := gob.NewDecoder(conn)
	if err := decoder.Decode(&regResult); err != nil {
		return nil, fmt.Errorf("unable to decode&receive the registration result: %w", err)
	}
	if regResult.Error != "" {
		return nil, fmt.Errorf("registration error: %s", regResult.Error)
	}
	logger.Default().Tracef("successfully registered the process '%s'", myName)

	return &Client{
		Conn:     conn,
		Password: password,
	}, nil
}

func (c *Client) SendMessage(
	ctx context.Context,
	dst ProcessName,
	content any,
) error {
	logger.Debugf(ctx, "SendMessage(ctx, '%s', %T)", dst, content)
	defer logger.Debugf(ctx, "/SendMessage(ctx, '%s', %T)", dst, content)
	encoder := gob.NewEncoder(c.Conn)
	msg := MessageToMain{
		Password:    c.Password,
		Destination: dst,
		Content:     content,
	}
	c.WriteLocker.Lock()
	defer c.WriteLocker.Unlock()
	err := encoder.Encode(msg)
	logger.Tracef(ctx, "sending message %#+v: %v", msg, err)
	if err != nil {
		return fmt.Errorf("unable to encode&send message %#+v: %w", msg, err)
	}
	return nil
}

func (c *Client) Close() error {
	return c.Conn.Close()
}

func (c *Client) Serve(
	ctx context.Context,
	onReceivedMessage OnReceivedMessageFunc,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	go func() {
		<-ctx.Done()
		err := c.Close()
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

		if err := c.ReadOne(ctx, onReceivedMessage); err != nil {
			return err
		}
	}
}

func (c *Client) ReadOne(
	ctx context.Context,
	onReceivedMessage OnReceivedMessageFunc,
) error {
	var msg MessageFromMain
	decoder := gob.NewDecoder(c.Conn)
	err := decoder.Decode(&msg)
	if err != nil {
		return fmt.Errorf("unable to receive&decode message: %w", err)
	}

	if err := onReceivedMessage(ctx, msg.Source, msg.Content); err != nil {
		return fmt.Errorf("unable to process the message '%#+v': %w", msg, err)
	}

	return nil
}
