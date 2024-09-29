package streamforward

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/xaionaro-go/streamctl/pkg/xsync"

	"github.com/facebookincubator/go-belt/tool/logger"
)

type config = RecodingConfig
type VideoRecorder struct {
	config

	mutex xsync.Mutex

	closer *astikit.Closer

	currentPacket *astiav.Packet

	formatContextInput  *astiav.FormatContext
	formatContextOutput *astiav.FormatContext
}

type VideoTrackID uint
type AudioTrackID uint

type Output interface {
	WriteFrame(ctx context.Context) error
}

func New(
	cfg RecodingConfig,
	output Output,
) (*VideoRecorder, error) {
	r := &VideoRecorder{
		config: cfg,
		closer: astikit.NewCloser(),
	}
	if err := r.init(); err != nil {
		r.closer.Close()
		return nil, err
	}
	return r, nil
}

func toAstiavLogLevel(level logger.Level) astiav.LogLevel {
	switch level {
	case logger.LevelUndefined:
		return astiav.LogLevelQuiet
	case logger.LevelPanic:
		return astiav.LogLevelPanic
	case logger.LevelFatal:
		return astiav.LogLevelFatal
	case logger.LevelError:
		return astiav.LogLevelError
	case logger.LevelWarning:
		return astiav.LogLevelWarning
	case logger.LevelInfo:
		return astiav.LogLevelInfo
	case logger.LevelDebug:
		return astiav.LogLevelVerbose
	case logger.LevelTrace:
		return astiav.LogLevelDebug
	}
	return astiav.LogLevelWarning
}

func fromAstiavLogLevel(level astiav.LogLevel) logger.Level {
	switch level {
	case astiav.LogLevelQuiet:
		return logger.LevelUndefined
	case astiav.LogLevelFatal:
		return logger.LevelFatal
	case astiav.LogLevelPanic:
		return logger.LevelPanic
	case astiav.LogLevelError:
		return logger.LevelError
	case astiav.LogLevelWarning:
		return logger.LevelWarning
	case astiav.LogLevelInfo:
		return logger.LevelInfo
	case astiav.LogLevelVerbose:
		return logger.LevelDebug
	case astiav.LogLevelDebug:
		return logger.LevelTrace
	}
	return logger.LevelWarning
}

func (r *VideoRecorder) init() error {
	logger := logger.Default().WithField("module", "astiav")

	astiav.SetLogLevel(toAstiavLogLevel(logger.Level()))
	astiav.SetLogCallback(func(c astiav.Classer, l astiav.LogLevel, fmt, msg string) {
		var cs string
		if c != nil {
			if cl := c.Class(); cl != nil {
				cs = " - class: " + cl.String()
			}
		}
		logger.Logf(
			fromAstiavLogLevel(l),
			"%s%s",
			strings.TrimSpace(msg), cs,
		)
	})

	r.closer = astikit.NewCloser()

	r.currentPacket = astiav.AllocPacket()
	r.closer.Add(r.currentPacket.Free)

	r.formatContextInput = astiav.AllocFormatContext()
	r.closer.Add(r.formatContextInput.Free)

	var err error
	r.formatContextOutput, err = astiav.AllocOutputFormatContext(nil, "flv", "")
	if err != nil {
		return fmt.Errorf("unable to initialize the output context: %w", err)
	}
	r.closer.Add(r.formatContextOutput.Free)

	r.formatContextInput.WriteInterleavedFrame()
	return nil
}

func (r *VideoRecorder) initInput(
	ctx context.Context,
) (err error) {
	r.mutex.Do(ctx, func() {
		err = r.doInitInput()
	})
	return
}

func (r *VideoRecorder) doInitInput() (err error) {
	// Find stream info
	if err = inputFormatContext.FindStreamInfo(nil); err != nil {
		err = fmt.Errorf("main: finding stream info failed: %w", err)
		return
	}

	// Loop through streams
	for _, is := range inputFormatContext.Streams() {
		// Only process audio or video
		if is.CodecParameters().MediaType() != astiav.MediaTypeAudio &&
			is.CodecParameters().MediaType() != astiav.MediaTypeVideo {
			continue
		}

		// Create stream
		s := &stream{inputStream: is}

		// Find decoder
		if s.decCodec = astiav.FindDecoder(is.CodecParameters().CodecID()); s.decCodec == nil {
			err = errors.New("main: codec is nil")
			return
		}

		// Alloc codec context
		if s.decCodecContext = astiav.AllocCodecContext(s.decCodec); s.decCodecContext == nil {
			err = errors.New("main: codec context is nil")
			return
		}
		c.Add(s.decCodecContext.Free)

		// Update codec context
		if err = is.CodecParameters().ToCodecContext(s.decCodecContext); err != nil {
			err = fmt.Errorf("main: updating codec context failed: %w", err)
			return
		}

		// Set framerate
		if is.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			s.decCodecContext.SetFramerate(inputFormatContext.GuessFrameRate(is, nil))
		}

		// Open codec context
		if err = s.decCodecContext.Open(s.decCodec, nil); err != nil {
			err = fmt.Errorf("main: opening codec context failed: %w", err)
			return
		}

		// Alloc frame
		s.decFrame = astiav.AllocFrame()
		c.Add(s.decFrame.Free)

		// Store stream
		streams[is.Index()] = s
	}
	return
}

func openOutputFile() (err error) {

	// Loop through streams
	for _, is := range inputFormatContext.Streams() {
		// Get stream
		s, ok := streams[is.Index()]
		if !ok {
			continue
		}

		// Create output stream
		if s.outputStream = outputFormatContext.NewStream(nil); s.outputStream == nil {
			err = errors.New("main: output stream is nil")
			return
		}

		// Get codec id
		codecID := astiav.CodecIDMpeg4
		if s.decCodecContext.MediaType() == astiav.MediaTypeAudio {
			codecID = astiav.CodecIDAac
		}

		// Find encoder
		if s.encCodec = astiav.FindEncoder(codecID); s.encCodec == nil {
			err = errors.New("main: codec is nil")
			return
		}

		// Alloc codec context
		if s.encCodecContext = astiav.AllocCodecContext(s.encCodec); s.encCodecContext == nil {
			err = errors.New("main: codec context is nil")
			return
		}
		c.Add(s.encCodecContext.Free)

		// Update codec context
		if s.decCodecContext.MediaType() == astiav.MediaTypeAudio {
			if v := s.encCodec.ChannelLayouts(); len(v) > 0 {
				s.encCodecContext.SetChannelLayout(v[0])
			} else {
				s.encCodecContext.SetChannelLayout(s.decCodecContext.ChannelLayout())
			}
			s.encCodecContext.SetSampleRate(s.decCodecContext.SampleRate())
			if v := s.encCodec.SampleFormats(); len(v) > 0 {
				s.encCodecContext.SetSampleFormat(v[0])
			} else {
				s.encCodecContext.SetSampleFormat(s.decCodecContext.SampleFormat())
			}
			s.encCodecContext.SetTimeBase(astiav.NewRational(1, s.encCodecContext.SampleRate()))
		} else {
			s.encCodecContext.SetHeight(s.decCodecContext.Height())
			if v := s.encCodec.PixelFormats(); len(v) > 0 {
				s.encCodecContext.SetPixelFormat(v[0])
			} else {
				s.encCodecContext.SetPixelFormat(s.decCodecContext.PixelFormat())
			}
			s.encCodecContext.SetSampleAspectRatio(s.decCodecContext.SampleAspectRatio())
			s.encCodecContext.SetTimeBase(s.decCodecContext.Framerate().Invert())
			s.encCodecContext.SetWidth(s.decCodecContext.Width())
		}

		// Update flags
		if s.decCodecContext.Flags().Has(astiav.CodecContextFlagGlobalHeader) {
			s.encCodecContext.SetFlags(s.encCodecContext.Flags().Add(astiav.CodecContextFlagGlobalHeader))
		}

		// Open codec context
		if err = s.encCodecContext.Open(s.encCodec, nil); err != nil {
			err = fmt.Errorf("main: opening codec context failed: %w", err)
			return
		}

		// Update codec parameters
		if err = s.outputStream.CodecParameters().FromCodecContext(s.encCodecContext); err != nil {
			err = fmt.Errorf("main: updating codec parameters failed: %w", err)
			return
		}

		// Update stream
		s.outputStream.SetTimeBase(s.encCodecContext.TimeBase())
	}

	// If this is a file, we need to use an io context
	if !outputFormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		// Open io context
		var ioContext *astiav.IOContext
		if ioContext, err = astiav.OpenIOContext(*output, astiav.NewIOContextFlags(astiav.IOContextFlagWrite)); err != nil {
			err = fmt.Errorf("main: opening io context failed: %w", err)
			return
		}
		c.AddWithError(ioContext.Close)

		// Update output format context
		outputFormatContext.SetPb(ioContext)
	}

	// Write header
	if err = outputFormatContext.WriteHeader(nil); err != nil {
		err = fmt.Errorf("main: writing header failed: %w", err)
		return
	}
	return
}

func initFilters() (err error) {
	// Loop through output streams
	for _, s := range streams {
		// Alloc graph
		if s.filterGraph = astiav.AllocFilterGraph(); s.filterGraph == nil {
			err = errors.New("main: graph is nil")
			return
		}
		c.Add(s.filterGraph.Free)

		// Alloc outputs
		outputs := astiav.AllocFilterInOut()
		if outputs == nil {
			err = errors.New("main: outputs is nil")
			return
		}
		c.Add(outputs.Free)

		// Alloc inputs
		inputs := astiav.AllocFilterInOut()
		if inputs == nil {
			err = errors.New("main: inputs is nil")
			return
		}
		c.Add(inputs.Free)

		// Switch on media type
		var args astiav.FilterArgs
		var buffersrc, buffersink *astiav.Filter
		var content string
		switch s.decCodecContext.MediaType() {
		case astiav.MediaTypeAudio:
			args = astiav.FilterArgs{
				"channel_layout": s.decCodecContext.ChannelLayout().String(),
				"sample_fmt":     s.decCodecContext.SampleFormat().Name(),
				"sample_rate":    strconv.Itoa(s.decCodecContext.SampleRate()),
				"time_base":      s.decCodecContext.TimeBase().String(),
			}
			buffersrc = astiav.FindFilterByName("abuffer")
			buffersink = astiav.FindFilterByName("abuffersink")
			content = fmt.Sprintf("aformat=sample_fmts=%s:channel_layouts=%s", s.encCodecContext.SampleFormat().Name(), s.encCodecContext.ChannelLayout().String())
		default:
			args = astiav.FilterArgs{
				"pix_fmt":      strconv.Itoa(int(s.decCodecContext.PixelFormat())),
				"pixel_aspect": s.decCodecContext.SampleAspectRatio().String(),
				"time_base":    s.inputStream.TimeBase().String(),
				"video_size":   strconv.Itoa(s.decCodecContext.Width()) + "x" + strconv.Itoa(s.decCodecContext.Height()),
			}
			buffersrc = astiav.FindFilterByName("buffer")
			buffersink = astiav.FindFilterByName("buffersink")
			content = fmt.Sprintf("format=pix_fmts=%s", s.encCodecContext.PixelFormat().Name())
		}

		// Check filters
		if buffersrc == nil {
			err = errors.New("main: buffersrc is nil")
			return
		}
		if buffersink == nil {
			err = errors.New("main: buffersink is nil")
			return
		}

		// Create filter contexts
		if s.buffersrcContext, err = s.filterGraph.NewFilterContext(buffersrc, "in", args); err != nil {
			err = fmt.Errorf("main: creating buffersrc context failed: %w", err)
			return
		}
		if s.buffersinkContext, err = s.filterGraph.NewFilterContext(buffersink, "out", nil); err != nil {
			err = fmt.Errorf("main: creating buffersink context failed: %w", err)
			return
		}

		// Update outputs
		outputs.SetName("in")
		outputs.SetFilterContext(s.buffersrcContext)
		outputs.SetPadIdx(0)
		outputs.SetNext(nil)

		// Update inputs
		inputs.SetName("out")
		inputs.SetFilterContext(s.buffersinkContext)
		inputs.SetPadIdx(0)
		inputs.SetNext(nil)

		// Parse
		if err = s.filterGraph.Parse(content, inputs, outputs); err != nil {
			err = fmt.Errorf("main: parsing filter failed: %w", err)
			return
		}

		// Configure
		if err = s.filterGraph.Configure(); err != nil {
			err = fmt.Errorf("main: configuring filter failed: %w", err)
			return
		}

		// Alloc frame
		s.filterFrame = astiav.AllocFrame()
		c.Add(s.filterFrame.Free)

		// Alloc packet
		s.encPkt = astiav.AllocPacket()
		c.Add(s.encPkt.Free)
	}
	return
}

func (r *VideoRecorder) WriteVideoFrame(
	ctx context.Context,
	trackID VideoTrackID,
	ts time.Time,
	frame image.Image,
) error {

}

func (r *VideoRecorder) WriteAudioValue(
	ctx context.Context,
	trackID AudioTrackID,
	ts time.Time,
	value float64,
) error {

}
