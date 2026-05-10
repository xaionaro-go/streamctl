package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTranslatorChain_ClearHistory_emptiesHistory locks in the contract:
// after recording N entries, ClearHistory drops all of them and returns N.
// A subsequent ClearHistory must return 0 because there is nothing left.
func TestTranslatorChain_ClearHistory_emptiesHistory(t *testing.T) {
	tc := NewTranslatorChain("English", 10, nil)
	ctx := context.Background()

	tc.addToHistory(ctx, "alice", "hi")
	tc.addToHistory(ctx, "bob", "hello")
	tc.addToHistory(ctx, "carol", "hola")

	dropped := tc.ClearHistory(ctx)
	require.Equal(t, int32(3), dropped, "all three entries must be dropped")

	// A second call must report zero — proving the slice was actually cleared
	// (not just truncated to nil and re-grown by the next addToHistory).
	dropped = tc.ClearHistory(ctx)
	assert.Equal(t, int32(0), dropped, "no entries left after a clear")

	// formatHistory must produce an empty string (no <user> message lines).
	assert.Equal(t, "", tc.formatHistory(ctx),
		"formatHistory must reflect the cleared state")
}

// TestTranslatorChain_ClearHistory_emptyChain covers the bootstrap case: a
// freshly constructed chain has no history, so ClearHistory returns 0
// without panicking.
func TestTranslatorChain_ClearHistory_emptyChain(t *testing.T) {
	tc := NewTranslatorChain("English", 10, nil)
	dropped := tc.ClearHistory(context.Background())
	assert.Equal(t, int32(0), dropped)
}
