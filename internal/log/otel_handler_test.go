package log

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
)

type recordingHandler struct {
	records *[]string
}

func (r recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (r recordingHandler) Handle(_ context.Context, rec slog.Record) error {
	*r.records = append(*r.records, rec.Message)
	return nil
}

func (r recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return r }
func (r recordingHandler) WithGroup(string) slog.Handler      { return r }

func TestMultiHandlerGatesOnLevel(t *testing.T) {
	var got []string
	h := &multiHandler{
		level:    slog.LevelWarn,
		handlers: []slog.Handler{recordingHandler{&got}, recordingHandler{&got}},
	}
	logger := slog.New(h)

	logger.Debug("debug msg")
	logger.Warn("warn msg")

	assert.Equal(t, []string{"warn msg", "warn msg"}, got)
}

func TestOtelExportDisabledByDefault(t *testing.T) {
	var buf bytes.Buffer
	conf := NewConfig()
	conf.Format = "json"
	require.False(t, conf.Otel.Enabled)

	logger, err := New(&buf, ifs.OS(), conf)
	require.NoError(t, err)
	logger.Info("hello")

	assert.Contains(t, buf.String(), `"msg":"hello"`)
}

func TestOtelHandlerConstructs(t *testing.T) {
	h, shutdown, err := newOtelHandler(OtelConfig{
		Enabled: true,
		GRPC:    []OtelCollector{{Address: "localhost:4317", Secure: false}},
		HTTP:    []OtelCollector{{Address: "localhost:4318", Secure: false}},
		Tags:    map[string]string{"env": "test"},
	})
	require.NoError(t, err)
	require.NotNil(t, h)
	require.NotNil(t, shutdown)
}
