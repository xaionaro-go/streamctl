package streamd

import (
	"context"
	"testing"

	"github.com/facebookincubator/go-belt"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/streamctl/pkg/llm/types"
	"github.com/xaionaro-go/streamctl/pkg/streamd/config"
)

type mockLLM struct {
	types.LLM
}

func (m *mockLLM) Generate(ctx context.Context, prompt string) (string, error) {
	return "AI response: " + prompt, nil
}

func (m *mockLLM) Close() error {
	return nil
}

func TestLLMGenerate(t *testing.T) {
	l := &llm{
		backend: &mockLLM{},
	}
	resp, err := l.Generate(context.Background(), "test prompt")
	require.NoError(t, err)
	require.Equal(t, "AI response: test prompt", resp)
}

func TestLLMGenerateIntegration(t *testing.T) {
	d := &StreamD{}
	d.llm = &llm{
		streamD: d,
		backend: &mockLLM{},
	}

	resp, err := d.LLMGenerate(context.Background(), "hello")
	require.NoError(t, err)
	require.Equal(t, "AI response: hello", resp)
}

func TestLLMGenerateSystem(t *testing.T) {
	// In a real system test we would check if LLM endpoint is reachable.
	// We mock it for the test.
	d := &StreamD{}
	d.llm = &llm{
		streamD: d,
		backend: &mockLLM{},
	}

	// Verify that the daemon reports LLM as available/functional via Generate
	_, err := d.LLMGenerate(context.Background(), "ping")
	require.NoError(t, err)
}

func TestLLMInit(t *testing.T) {
	cfg := config.NewConfig()
	d, _ := New(cfg, &mockUI{}, nil, belt.New())
	ctx := context.Background()

	err := d.initLLMs(ctx)
	require.NoError(t, err)
	require.NotNil(t, d.llm)
}
