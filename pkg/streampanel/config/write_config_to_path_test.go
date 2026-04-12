package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteConfigToPath_SequentialWriteProducesCorrectContent verifies
// that writing a config and reading it back yields the original data.
// Regression test for Task #40 (fsync before rename).
func TestWriteConfigToPath_SequentialWriteProducesCorrectContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	ctx := context.Background()

	cfg := Config{
		RemoteStreamDAddr: "192.168.1.100:8080",
	}

	require.NoError(t, WriteConfigToPath(ctx, cfgPath, cfg))

	var readBack Config
	require.NoError(t, ReadConfigFromPath[Config](cfgPath, &readBack))
	assert.Equal(t, cfg.RemoteStreamDAddr, readBack.RemoteStreamDAddr,
		"read-back config must match written config")
}

// TestWriteConfigToPath_OverwriteReplacesContent verifies that a second
// write to the same path replaces the previous content completely.
func TestWriteConfigToPath_OverwriteReplacesContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	ctx := context.Background()

	cfg1 := Config{RemoteStreamDAddr: "first-addr"}
	require.NoError(t, WriteConfigToPath(ctx, cfgPath, cfg1))

	cfg2 := Config{RemoteStreamDAddr: "second-addr"}
	require.NoError(t, WriteConfigToPath(ctx, cfgPath, cfg2))

	var readBack Config
	require.NoError(t, ReadConfigFromPath[Config](cfgPath, &readBack))
	assert.Equal(t, "second-addr", readBack.RemoteStreamDAddr,
		"second write must fully replace the first")
	assert.NotEqual(t, "first-addr", readBack.RemoteStreamDAddr,
		"first write content must not remain")
}

// TestWriteConfigToPath_NoTempFilesLeaked verifies that no .new
// temporary files remain in the directory after a successful write.
func TestWriteConfigToPath_NoTempFilesLeaked(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	ctx := context.Background()

	cfg := Config{RemoteStreamDAddr: "leak-test"}
	require.NoError(t, WriteConfigToPath(ctx, cfgPath, cfg))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".new",
			"temp file %q must not remain after successful write", e.Name())
	}
}

// TestWriteConfigToPath_SerializedConcurrentWritesProduceValidConfig
// verifies that rapid serialized writes (simulating the real usage
// where callers hold a mutex) always produce a valid, non-empty file.
// WriteConfigToPath shares the .new temp path so callers must
// serialize; this test verifies the fsync+rename atomicity under rapid
// sequential writes from multiple goroutines.
func TestWriteConfigToPath_SerializedConcurrentWritesProduceValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	ctx := context.Background()

	const goroutines = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			cfg := Config{
				RemoteStreamDAddr: fmt.Sprintf("addr-from-%d", idx),
			}
			mu.Lock()
			_ = WriteConfigToPath(ctx, cfgPath, cfg)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// The file must exist and be a valid, non-empty config.
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err, "config file must exist after serialized writes")
	assert.NotEmpty(t, data, "config file must not be zero-length")

	var readBack Config
	require.NoError(t, ReadConfigFromPath[Config](cfgPath, &readBack),
		"config file must be valid YAML after serialized writes")
	assert.Contains(t, readBack.RemoteStreamDAddr, "addr-from-",
		"config must contain a written field from one of the goroutines")

	// No temp files leaked.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".new",
			"temp file %q must not remain after writes", e.Name())
	}
}

// TestWriteConfigToPath_FileNeverZeroLength verifies that the config
// file is never zero-length, even right after a write. This property
// holds because fsync flushes data to disk before rename atomically
// replaces the old file.
func TestWriteConfigToPath_FileNeverZeroLength(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	ctx := context.Background()

	// Write 5 times in sequence, checking file size each time.
	for i := 0; i < 5; i++ {
		cfg := Config{RemoteStreamDAddr: "addr-iteration"}
		require.NoError(t, WriteConfigToPath(ctx, cfgPath, cfg))

		info, err := os.Stat(cfgPath)
		require.NoError(t, err)
		assert.Greater(t, info.Size(), int64(0),
			"config file must not be zero-length after write (iteration %d)", i)
	}
}
