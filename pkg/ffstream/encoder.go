package ffstream

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/streamctl/pkg/ffstream/types"
	"github.com/xaionaro-go/streamctl/pkg/recoder/libav/recoder"
)

type Encoder struct {
	recoder.CommonsEncoder

	Config EncoderConfig

	Decoder                     *recoder.Decoder
	prevEncodeTS                time.Time
	locker                      sync.Mutex
	inputStreams                map[*recoder.Input]map[int]*astiav.Stream
	skippedVideoFrame           bool
	videoAveragerBufferConsumed int64
}

var _ recoder.Encoder = (*Encoder)(nil)

type CodecConfig = types.CodecConfig
type EncoderConfig = types.EncoderConfig

func NewEncoder() *Encoder {
	return &Encoder{
		inputStreams: map[*recoder.Input]map[int]*astiav.Stream{},
	}
}

func (e *Encoder) Configure(
	ctx context.Context,
	cfg EncoderConfig,
) error {
	if cfg.Audio.CodecName != "copy" {
		return fmt.Errorf("currently we support only audio codec 'copy', but the selected one is '%s'", cfg.Audio.CodecName)
	}
	if len(cfg.Audio.CustomOptions) != 0 {
		return fmt.Errorf("currently we do not support custom options to the audio codec, but received: %v", cfg.Audio.CustomOptions)
	}
	if cfg.Audio.AverageBitRate != 0 {
		return fmt.Errorf("bitrate limitation for the audio track is not supported, yet")
	}

	if cfg.Video.CodecName != "copy" {
		return fmt.Errorf("currently we support only audio codec 'copy', but the selected one is '%s'", cfg.Video.CodecName)
	}
	for _, opt := range cfg.Video.CustomOptions {
		switch opt.Key {
		case "bw":
			if cfg.Video.AverageBitRate != 0 {
				return fmt.Errorf("the average bitrate is already configured to be '%d', but I also received option 'bw' (with value '%s'); please don't use both ways to configure the bitrate", cfg.Video.AverageBitRate, opt.Value)
			}
			bitRate, err := strconv.ParseUint(opt.Value, 10, 64)
			if err != nil {
				return fmt.Errorf("unable to parse value '%s' as an unsigned integer: %w", opt.Value, err)
			}
			cfg.Video.AverageBitRate = bitRate
		default:
			return fmt.Errorf("currently we do not support any custom options to the video codec besides 'bw', but we received an option '%s'", opt.Key)
		}
	}

	if cfg.Video.AverageBitRate != 0 && cfg.Video.AveragingPeriod == 0 {
		cfg.Video.AveragingPeriod = time.Second * 10
		logger.Warnf(ctx, "AveragingPeriod is not set, defaulting to %v", cfg.Video.AveragingPeriod)
	}

	e.locker.Lock()
	defer e.locker.Unlock()
	e.Config = cfg
	return nil
}

func (e *Encoder) Encode(
	ctx context.Context,
	input recoder.EncoderInput,
) (_ret *recoder.EncoderOutput, _err error) {
	e.BytesCountRead.Add(uint64(input.Packet.Size()))
	defer func() {
		if _ret != nil {
			e.BytesCountWrote.Add(uint64(_ret.Size()))
		}
	}()
	e.locker.Lock()
	defer e.locker.Unlock()

	inputStreams := e.inputStreams[input.Input]
	if inputStreams == nil {
		inputStreams = map[int]*astiav.Stream{}
		e.inputStreams[input.Input] = inputStreams
	}

	inputStreamIdx := input.Packet.StreamIndex()

	inputStream := inputStreams[inputStreamIdx]
	if inputStream == nil {
		for _, stream := range input.Input.Streams() {
			inputStreams[stream.Index()] = stream
		}
	}

	inputStream = inputStreams[inputStreamIdx]
	if inputStream == nil {
		return nil, fmt.Errorf("unable to find a stream with index #%d", inputStreamIdx)
	}

	mediaType := inputStream.CodecParameters().MediaType()
	switch mediaType {
	case astiav.MediaTypeVideo:
		return e.encodeVideoPacket(ctx, input, inputStream)
	case astiav.MediaTypeAudio:
		return e.encodeAudioPacket(ctx, input, inputStream)
	default:
		logger.Tracef(ctx, "an uninteresting packet of type %s", mediaType)
		// we don't care about everything else
		return nil, nil
	}
}

func (e *Encoder) encodeVideoPacket(
	ctx context.Context,
	input recoder.EncoderInput,
	inputStream *astiav.Stream,
) (_ret *recoder.EncoderOutput, _err error) {
	logger.Tracef(
		ctx,
		"a video packet (pos:%d, pts:%d, dur:%d)",
		input.Packet.Pos(), input.Packet.Pts(), input.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "a video packet: %v", _err) }()
	e.FramesRead.VideoUnprocessed.Add(1)

	if e.Config.Video.AverageBitRate == 0 {
		e.videoAveragerBufferConsumed = 0
		e.FramesWrote.VideoUnprocessed.Add(1)
		return &recoder.EncoderOutput{
			Packet: recoder.ClonePacketAsWritable(input.Packet),
			Stream: inputStream,
		}, nil
	}

	now := time.Now()
	prevTS := e.prevEncodeTS
	e.prevEncodeTS = now

	tsDiff := now.Sub(prevTS)
	allowMoreBits := 1 + int64(tsDiff.Seconds()*float64(e.Config.Video.AverageBitRate))

	e.videoAveragerBufferConsumed -= allowMoreBits
	if e.videoAveragerBufferConsumed < 0 {
		e.videoAveragerBufferConsumed = 0
	}

	pktSize := input.Packet.Size()
	averagingBuffer := int64(e.Config.Video.AveragingPeriod.Seconds() * float64(e.Config.Video.AverageBitRate))
	consumedWithPacket := e.videoAveragerBufferConsumed + int64(pktSize)*8
	if consumedWithPacket > averagingBuffer {
		e.skippedVideoFrame = true
		logger.Tracef(ctx, "skipping a frame to reduce the bitrate: %d > %d", consumedWithPacket, averagingBuffer)
		return nil, nil
	}

	if e.skippedVideoFrame {
		isKeyFrame := input.Packet.Flags().Has(astiav.PacketFlagKey)
		if !isKeyFrame {
			logger.Tracef(ctx, "skipping a non-key frame (BTW, the consumedWithPacket is %d/%d)", consumedWithPacket, averagingBuffer)
			return nil, nil
		}
	}

	e.skippedVideoFrame = false
	e.videoAveragerBufferConsumed = consumedWithPacket
	e.FramesWrote.VideoUnprocessed.Add(1)
	return &recoder.EncoderOutput{
		Packet: recoder.ClonePacketAsWritable(input.Packet),
		Stream: inputStream,
	}, nil
}

func (e *Encoder) encodeAudioPacket(
	ctx context.Context,
	input recoder.EncoderInput,
	inputStream *astiav.Stream,
) (_ret *recoder.EncoderOutput, _err error) {
	logger.Tracef(
		ctx,
		"an audio packet (pos:%d, pts:%d, dts:%d, dur:%d)",
		input.Packet.Pos(), input.Packet.Pts(), input.Packet.Dts(), input.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "an audio packet: %v", _err) }()
	e.FramesRead.AudioUnprocessed.Add(1)
	e.FramesWrote.AudioUnprocessed.Add(1)
	return &recoder.EncoderOutput{
		Packet: recoder.ClonePacketAsWritable(input.Packet),
		Stream: inputStream,
	}, nil
}

func (e *Encoder) Close() error {
	return nil
}
