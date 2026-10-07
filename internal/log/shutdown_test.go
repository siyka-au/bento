package log

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
)

func TestShutdownOtelDisabledIsNoop(t *testing.T) {
	var buf bytes.Buffer
	conf := NewConfig()

	logger, err := New(&buf, ifs.OS(), conf)
	require.NoError(t, err)

	assert.NoError(t, Shutdown(context.Background(), logger))
}

func TestShutdownForwardsThroughWrappers(t *testing.T) {
	s := NewBentoLogAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))

	var called bool
	s.shutdown = func(context.Context) error {
		called = true
		return nil
	}

	logger := TeeLogger(WrapErrPromoter(s), Noop())

	require.NoError(t, Shutdown(context.Background(), logger))
	assert.True(t, called, "Shutdown should have been forwarded through TeeLogger and WrapErrPromoter to the adapter")
}
