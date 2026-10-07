package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func attrMap(r *Resource) map[string]string {
	m := map[string]string{}
	for _, kv := range r.OTelResource().Attributes() {
		m[string(kv.Key)] = kv.Value.Emit()
	}
	return m
}

func baseConf(label string) Config {
	return Config{
		Label: label,
		OpenTelemetryCollector: OTelCollectorConfig{
			GRPC: []Collector{{Address: "localhost:4317"}},
			Metrics: MetricsConfig{
				PushInterval: "10s",
			},
		},
	}
}

func TestNewResourceDefaultServiceAttributes(t *testing.T) {
	r, err := newResource(baseConf("test"), "1.2.3")
	require.NoError(t, err)

	attrs := attrMap(r)
	assert.Equal(t, "bento", attrs["service.name"])
	assert.Equal(t, "1.2.3", attrs["service.version"])
}

func TestNewResourceNoEngineVersionOmitsServiceVersion(t *testing.T) {
	r, err := newResource(baseConf("test"), "")
	require.NoError(t, err)

	attrs := attrMap(r)
	assert.Equal(t, "bento", attrs["service.name"])
	_, ok := attrs["service.version"]
	assert.False(t, ok)
}

func TestNewResourceUserTagsOverrideDefaults(t *testing.T) {
	conf := baseConf("test")
	conf.OpenTelemetryCollector.Tags = map[string]string{
		"service.name":    "custom-service",
		"service.version": "9.9.9",
		"env":             "prod",
	}

	r, err := newResource(conf, "1.2.3")
	require.NoError(t, err)

	attrs := attrMap(r)
	assert.Equal(t, "custom-service", attrs["service.name"])
	assert.Equal(t, "9.9.9", attrs["service.version"])
	assert.Equal(t, "prod", attrs["env"])
}

func TestResourceLabel(t *testing.T) {
	r, err := newResource(baseConf("my-label"), "")
	require.NoError(t, err)
	assert.Equal(t, "my-label", r.Label())
}

func TestResourceProvidersConstructAndShutdown(t *testing.T) {
	conf := baseConf("test")
	r, err := newResource(conf, "1.2.3")
	require.NoError(t, err)

	ctx := context.Background()

	tp, err := r.TracerProvider(ctx)
	require.NoError(t, err)
	require.NotNil(t, tp)

	mp, err := r.MeterProvider(ctx)
	require.NoError(t, err)
	require.NotNil(t, mp)

	lp, err := r.LoggerProvider(ctx)
	require.NoError(t, err)
	require.NotNil(t, lp)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// A dead endpoint may cause Shutdown to return an error; this only
	// verifies that it returns promptly rather than hanging the test.
	done := make(chan struct{})
	go func() {
		_ = tp.Shutdown(shutdownCtx)
		_ = mp.Shutdown(shutdownCtx)
		_ = lp.Shutdown(shutdownCtx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("provider shutdown did not return in time")
	}
}
