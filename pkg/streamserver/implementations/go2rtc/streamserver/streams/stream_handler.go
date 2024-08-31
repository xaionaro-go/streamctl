package streams

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/xaionaro-go/streamctl/pkg/xsync"
)

func (s *StreamHandler) Get(name string) *Stream {
	return s.streams[name]
}

var sanitize = regexp.MustCompile(`\s`)

// Validate - not allow creating dynamic streams with spaces in the source
func (s *StreamHandler) Validate(source string) error {
	if sanitize.MatchString(source) {
		return errors.New("streams: invalid dynamic source")
	}
	return nil
}

func (s *StreamHandler) New(name string, source any) (*Stream, error) {
	stream := s.NewStream(source)
	s.streams[name] = stream
	return stream, nil
}

func (s *StreamHandler) CreateOrUpdate(name string, source string) (*Stream, error) {
	ctx := context.TODO()
	return xsync.DoA2R2(ctx, &s.streamsMu, s.createOrUpdate, name, source)
}

func (s *StreamHandler) createOrUpdate(name string, source string) (*Stream, error) {
	// check if source links to some stream name from go2rtc
	if u, err := url.Parse(source); err == nil && u.Scheme == "rtsp" && len(u.Path) > 1 {
		rtspName := u.Path[1:]
		if stream, ok := s.streams[rtspName]; ok {
			if s.streams[name] != stream {
				// link (alias) streams[name] to streams[rtspName]
				s.streams[name] = stream
			}
			return stream, nil
		}
	}

	if stream, ok := s.streams[source]; ok {
		if name != source {
			// link (alias) streams[name] to streams[source]
			s.streams[name] = stream
		}
		return stream, nil
	}

	// check if src has supported scheme
	if !s.HasProducer(source) {
		return nil, fmt.Errorf("scheme is not supported")
	}

	// check an existing stream with this name
	if stream, ok := s.streams[name]; ok {
		stream.SetSource(source)
		return stream, nil
	}

	// create new stream with this name
	return s.New(name, source)
}

func (s *StreamHandler) GetAll() (names []string) {
	for name := range s.streams {
		names = append(names, name)
	}
	return
}

func (s *StreamHandler) Delete(id string) {
	delete(s.streams, id)
}

type StreamHandler struct {
	streams          map[string]*Stream
	streamsMu        xsync.Mutex
	consumerHandlers map[string]ConsumerHandler
	handlers         map[string]Handler
	redirects        map[string]Redirect
}

func NewStreamHandler() *StreamHandler {
	return &StreamHandler{
		streams:          map[string]*Stream{},
		streamsMu:        xsync.Mutex{},
		consumerHandlers: map[string]ConsumerHandler{},
		handlers:         map[string]Handler{},
		redirects:        map[string]Redirect{},
	}
}
