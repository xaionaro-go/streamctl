package streamd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/obs-grpc-proxy/protobuf/go/obs_grpc"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/streamctl/pkg/streamd/api"
	llmconfig "github.com/xaionaro-go/streamctl/pkg/streamd/config/llm"
	"github.com/xaionaro-go/streamctl/pkg/streamd/consts"
	"github.com/xaionaro-go/streamctl/pkg/streamtypes"
	"github.com/xaionaro-go/xsync"
)

const (
	subtitleChunkDuration = 5 * time.Second
	subtitleOBSSourceName = "Subtitles"
	whisperModel          = "whisper-1"
	maxTranscriptionLen   = 200
)

type subtitler struct {
	locker     xsync.Mutex
	streamD    *StreamD
	cancelFunc context.CancelFunc
	done       chan struct{}
}

func newSubtitler(d *StreamD) *subtitler {
	return &subtitler{streamD: d}
}

func (d *StreamD) initSubtitles(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initSubtitles")
	defer func() { logger.Debugf(ctx, "/initSubtitles: %v", _err) }()

	d.subtitler = newSubtitler(d)

	ch, err := d.SubscribeToVariable(ctx, consts.VarKeySubtitlesEnabled)
	if err != nil {
		return fmt.Errorf("unable to subscribe to '%s': %w", consts.VarKeySubtitlesEnabled, err)
	}

	observability.Go(ctx, func(ctx context.Context) {
		for value := range ch {
			enabled := string(value) == "true"
			logger.Debugf(ctx, "subtitles_enabled changed to %v", enabled)
			switch enabled {
			case true:
				d.subtitler.start(ctx)
			case false:
				d.subtitler.stop(ctx)
			}
		}
	})

	return nil
}

func (s *subtitler) start(
	ctx context.Context,
) {
	logger.Tracef(ctx, "subtitler.start")
	defer logger.Tracef(ctx, "/subtitler.start")

	s.locker.Do(ctx, func() {
		if s.cancelFunc != nil {
			return
		}

		loopCtx, cancelFunc := context.WithCancel(ctx)
		s.cancelFunc = cancelFunc
		s.done = make(chan struct{})

		done := s.done
		observability.Go(loopCtx, func(ctx context.Context) {
			defer close(done)
			s.loop(ctx)
		})
	})
}

func (s *subtitler) stop(
	ctx context.Context,
) {
	logger.Tracef(ctx, "subtitler.stop")
	defer logger.Tracef(ctx, "/subtitler.stop")

	var done chan struct{}
	s.locker.Do(ctx, func() {
		if s.cancelFunc != nil {
			s.cancelFunc()
			s.cancelFunc = nil
		}
		done = s.done
		s.done = nil
	})

	// Wait for the loop goroutine to finish before clearing, so there is
	// no race between the loop's updateOBSText and the clear below.
	if done != nil {
		<-done
	}

	// Clear the OBS text when stopping.
	if err := s.updateOBSText(ctx, ""); err != nil {
		logger.Warnf(ctx, "unable to clear subtitle text: %v", err)
	}
}

func (s *subtitler) loop(
	ctx context.Context,
) {
	logger.Debugf(ctx, "subtitler.loop")
	defer logger.Debugf(ctx, "/subtitler.loop")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streamURL, err := s.findActiveStreamURL(ctx)
		if err != nil {
			logger.Debugf(ctx, "no active stream for subtitles: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		audio, err := s.extractAudioChunk(ctx, streamURL)
		if err != nil {
			logger.Warnf(ctx, "unable to extract audio chunk: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}

		if len(audio) == 0 {
			continue
		}

		text, err := s.transcribeChunk(ctx, audio)
		if err != nil {
			logger.Warnf(ctx, "unable to transcribe audio chunk: %v", err)
			continue
		}

		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		if len(text) > maxTranscriptionLen {
			text = text[:maxTranscriptionLen]
		}

		if err := s.updateOBSText(ctx, text); err != nil {
			logger.Warnf(ctx, "unable to update OBS subtitle text: %v", err)
		}
	}
}

func (s *subtitler) findActiveStreamURL(
	ctx context.Context,
) (_ret string, _err error) {
	logger.Tracef(ctx, "findActiveStreamURL")
	defer func() { logger.Tracef(ctx, "/findActiveStreamURL: '%s', %v", _ret, _err) }()

	servers, err := s.streamD.ListStreamServers(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to list stream servers: %w", err)
	}

	streams, err := s.streamD.ListIncomingStreams(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to list incoming streams: %w", err)
	}

	var activeStreamID api.StreamID
	for _, stream := range streams {
		if stream.IsActive {
			activeStreamID = stream.StreamID
			break
		}
	}
	if activeStreamID == "" {
		return "", fmt.Errorf("no active incoming stream")
	}

	for _, srv := range servers {
		switch srv.Type {
		case streamtypes.ServerTypeRTMP:
			return fmt.Sprintf("rtmp://%s/%s", srv.ListenAddr, activeStreamID), nil
		case streamtypes.ServerTypeSRT:
			return fmt.Sprintf("srt://%s?streamid=%s", srv.ListenAddr, activeStreamID), nil
		}
	}

	return "", fmt.Errorf("no RTMP or SRT server found")
}

func (s *subtitler) extractAudioChunk(
	ctx context.Context,
	streamURL string,
) (_ret []byte, _err error) {
	logger.Tracef(ctx, "extractAudioChunk")
	defer func() { logger.Tracef(ctx, "/extractAudioChunk: len=%d, %v", len(_ret), _err) }()

	chunkSec := fmt.Sprintf("%.0f", subtitleChunkDuration.Seconds())

	// Extract a short audio chunk as 16kHz mono WAV (optimal for Whisper).
	cmd := exec.CommandContext(ctx,
		"ffmpeg",
		"-i", streamURL,
		"-t", chunkSec,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		"-f", "wav",
		"-y",
		"pipe:1",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w; stderr: %s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

func (s *subtitler) transcribeChunk(
	ctx context.Context,
	audioData []byte,
) (_ret string, _err error) {
	logger.Tracef(ctx, "transcribeChunk")
	defer func() { logger.Tracef(ctx, "/transcribeChunk: '%s', %v", _ret, _err) }()

	cfg, err := s.streamD.GetConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("unable to get config: %w", err)
	}

	endpoint := cfg.LLM.Endpoints.FirstByProvider(llmconfig.ProviderFasterWhisper)
	if endpoint == nil {
		endpoint = cfg.LLM.Endpoints["ChatGPT"]
	}
	if endpoint == nil {
		return "", fmt.Errorf("no whisper-capable LLM endpoint configured (set provider to %q or use endpoint name %q)",
			llmconfig.ProviderFasterWhisper, "ChatGPT")
	}

	apiURL := strings.TrimRight(endpoint.APIURL, "/")

	var transcriptionURL string
	switch endpoint.Provider {
	case llmconfig.ProviderFasterWhisper:
		if apiURL == "" {
			return "", fmt.Errorf("api_url is required for provider %q", endpoint.Provider)
		}
		transcriptionURL = apiURL + "/transcribe"
	default:
		if apiURL == "" {
			apiURL = "https://api.openai.com"
		}
		transcriptionURL = apiURL + "/v1/audio/transcriptions"
	}

	// Build multipart form data.
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	audioPart, err := writer.CreateFormFile("file", "chunk.wav")
	if err != nil {
		return "", fmt.Errorf("unable to create form file: %w", err)
	}
	if _, err := audioPart.Write(audioData); err != nil {
		return "", fmt.Errorf("unable to write audio data: %w", err)
	}

	if err := writer.WriteField("model", whisperModel); err != nil {
		return "", fmt.Errorf("unable to write model field: %w", err)
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("unable to write response_format field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("unable to close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, transcriptionURL, &body)
	if err != nil {
		return "", fmt.Errorf("unable to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+endpoint.APIKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("whisper returned %d: %s", resp.StatusCode, respBody)
	}

	return s.parseTranscriptionResponse(ctx, resp.Body, endpoint.Provider)
}

// parseTranscriptionResponse handles both OpenAI ({"text":"..."}) and
// faster-whisper ({"segments":[{"text":"..."}]}) response formats.
func (s *subtitler) parseTranscriptionResponse(
	ctx context.Context,
	body io.Reader,
	provider llmconfig.Provider,
) (string, error) {
	switch provider {
	case llmconfig.ProviderFasterWhisper:
		var result struct {
			Segments []struct {
				Text string `json:"text"`
			} `json:"segments"`
		}
		if err := json.NewDecoder(body).Decode(&result); err != nil {
			return "", fmt.Errorf("unable to decode faster-whisper response: %w", err)
		}
		var texts []string
		for _, seg := range result.Segments {
			t := strings.TrimSpace(seg.Text)
			if t != "" {
				texts = append(texts, t)
			}
		}
		return strings.Join(texts, " "), nil
	default:
		var result struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(body).Decode(&result); err != nil {
			return "", fmt.Errorf("unable to decode whisper response: %w", err)
		}
		return result.Text, nil
	}
}

func (s *subtitler) updateOBSText(
	ctx context.Context,
	text string,
) (_err error) {
	logger.Tracef(ctx, "updateOBSText")
	defer func() { logger.Tracef(ctx, "/updateOBSText: %v", _err) }()

	obsServer, obsServerClose, err := s.streamD.OBS(ctx)
	if obsServerClose != nil {
		defer obsServerClose()
	}
	if err != nil {
		return fmt.Errorf("unable to initialize OBS client: %w", err)
	}

	inputName := subtitleOBSSourceName
	_, err = obsServer.SetInputSettings(ctx, &obs_grpc.SetInputSettingsRequest{
		InputName: &inputName,
		InputSettings: &obs_grpc.AbstractObject{
			Fields: map[string]*obs_grpc.Any{
				"text": {
					Union: &obs_grpc.Any_String_{
						String_: []byte(text),
					},
				},
			},
		},
		Overlay: ptr(true),
	})
	if err != nil {
		return fmt.Errorf("unable to set OBS input settings for '%s': %w", subtitleOBSSourceName, err)
	}

	return nil
}
