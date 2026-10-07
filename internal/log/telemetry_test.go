package log

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
	"github.com/warpstreamlabs/bento/internal/telemetry"
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

func TestLoggerTelemetryDisabledByDefault(t *testing.T) {
	var buf bytes.Buffer
	conf := NewConfig()
	conf.Format = "json"
	require.Empty(t, conf.Telemetry)

	logger, err := New(&buf, ifs.OS(), conf)
	require.NoError(t, err)
	logger.Info("hello")

	assert.Contains(t, buf.String(), `"msg":"hello"`)
}

func telemetryTestRegistry(t *testing.T) *telemetry.Registry {
	t.Helper()
	reg, err := telemetry.NewRegistry([]telemetry.Config{
		{
			Label: "otel",
			OpenTelemetryCollector: telemetry.OTelCollectorConfig{
				GRPC: []telemetry.Collector{{Address: "localhost:4317", Secure: false}},
			},
		},
	}, "")
	require.NoError(t, err)
	return reg
}

func TestLoggerTelemetryExportsAlongsidePrimary(t *testing.T) {
	var buf bytes.Buffer
	conf := NewConfig()
	conf.Format = "json"
	conf.Telemetry = "otel"

	logger, err := New(&buf, ifs.OS(), conf, OptTelemetry(telemetryTestRegistry(t)))
	require.NoError(t, err)
	logger.Info("hello")

	assert.Contains(t, buf.String(), `"msg":"hello"`)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// The configured endpoint is dead, so Shutdown is expected to error on
	// flush; we only care that it returns within the deadline.
	_ = Shutdown(ctx, logger)
}

func TestLoggerTelemetryWithoutRegistryErrors(t *testing.T) {
	var buf bytes.Buffer
	conf := NewConfig()
	conf.Telemetry = "otel"

	_, err := New(&buf, ifs.OS(), conf)
	require.Error(t, err)
}

func TestLoggerTelemetryUnknownLabelErrors(t *testing.T) {
	var buf bytes.Buffer
	conf := NewConfig()
	conf.Telemetry = "does-not-exist"

	_, err := New(&buf, ifs.OS(), conf, OptTelemetry(telemetryTestRegistry(t)))
	require.Error(t, err)
	assert.ErrorContains(t, err, "does-not-exist")
}
