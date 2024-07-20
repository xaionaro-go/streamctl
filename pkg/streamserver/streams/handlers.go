package streams

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/xaionaro-go/streamctl/pkg/streamserver/server"
)

type Handler func(source string) (core.Producer, error)

func (s *StreamHandler) HandleFunc(scheme string, handler Handler) {
	s.handlers[scheme] = handler
}

func (s *StreamHandler) HasProducer(url string) bool {
	if i := strings.IndexByte(url, ':'); i > 0 {
		scheme := url[:i]

		if _, ok := s.handlers[scheme]; ok {
			return true
		}

		if _, ok := s.redirects[scheme]; ok {
			return true
		}
	}

	return false
}

type ErrNoProducer struct{}

func (err ErrNoProducer) Error() string {
	return "no producers"
}

func (s *StreamHandler) GetProducer(url string) (core.Producer, error) {
	i := strings.IndexByte(url, ':')
	if i <= 0 {
		return nil, fmt.Errorf("streams: empty scheme in URL: '%s'", url)
	}
	scheme := url[:i]

	if redirect, ok := s.redirects[scheme]; ok {
		location, err := redirect(url)
		if err != nil {
			return nil, err
		}
		if location != "" {
			return s.GetProducer(location)
		}
	}

	if handler, ok := s.handlers[scheme]; ok {
		return handler(url)
	}

	return nil, errors.New("streams: unsupported scheme in URL: " + url)
}

// Redirect can return: location URL or error or empty URL and error
type Redirect func(url string) (string, error)

func (s *StreamHandler) RedirectFunc(scheme string, redirect Redirect) {
	s.redirects[scheme] = redirect
}

func (s *StreamHandler) Location(url string) (string, error) {
	if i := strings.IndexByte(url, ':'); i > 0 {
		scheme := url[:i]

		if redirect, ok := s.redirects[scheme]; ok {
			return redirect(url)
		}
	}

	return "", nil
}

// TODO: rework

type ConsumerHandler func(url string) (core.Consumer, server.NumBytesReaderWroter, func(context.Context) error, error)

func (s *StreamHandler) HandleConsumerFunc(scheme string, handler ConsumerHandler) {
	s.consumerHandlers[scheme] = handler
}

func (s *StreamHandler) GetConsumer(url string) (core.Consumer, server.NumBytesReaderWroter, func(context.Context) error, error) {
	if i := strings.IndexByte(url, ':'); i > 0 {
		scheme := url[:i]

		if handler, ok := s.consumerHandlers[scheme]; ok {
			return handler(url)
		}
	}

	return nil, nil, nil, errors.New("streams: unsupported scheme: " + url)
}
