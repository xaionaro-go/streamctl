package whisper

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xaionaro-go/streamctl/pkg/speech"
)

var lineParseRegexp = regexp.MustCompile(`^([\-0-9]+) ([0-9+]+) (.*)$`)

func parseLine(
	_ context.Context,
	line string,
) (string, time.Duration, time.Duration, error) {
	r := lineParseRegexp.FindAllStringSubmatch(line, -1)
	if len(r) < 1 {
		return "", 0, 0, fmt.Errorf("expected %s, but received '%s' (len(r) == %d)", lineParseRegexp, line, len(r))
	}
	if len(r[0]) < 4 {
		return "", 0, 0, fmt.Errorf("expected %s, but received '%s' (len(r[0]) == %d)", lineParseRegexp, line, len(r[0]))
	}
	startTSStr := r[0][1]
	endTSStr := r[0][2]
	text := r[0][3]

	startTS, err := strconv.ParseInt(startTSStr, 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid StartTS '%s': %w", startTSStr, err)
	}

	endTS, err := strconv.ParseUint(endTSStr, 10, 64)
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid EndTS '%s': %w", endTSStr, err)
	}

	return text,
		time.Millisecond * time.Duration(startTS),
		time.Millisecond * time.Duration(endTS),
		nil
}

func parseMessage(
	ctx context.Context,
	message string,
) (string, []speech.TranscriptWord, error) {
	var words []speech.TranscriptWord
	var lines []string
	for _, line := range strings.Split(message, "\n") {
		if len(line) == 0 {
			continue
		}
		text, startTS, endTS, err := parseLine(ctx, line)
		if err != nil {
			return "", nil, fmt.Errorf("unable to parse line '%s': %w", line, err)
		}
		lines = append(lines, text)
		words = append(words, speech.TranscriptWord{
			StartTime:  startTS,
			EndTime:    endTS,
			Text:       text,
			Confidence: 0.5,
			Speaker:    "",
		})
	}
	return strings.Join(lines, " | "), words, nil
}
