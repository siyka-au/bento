package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
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

func TestTracerProviderSamplingRespectsSampledParent(t *testing.T) {
	conf := baseConf("test")
	conf.OpenTelemetryCollector.Traces.Sampling = SamplingConfig{Enabled: true, Ratio: 0}
	r, err := newResource(conf, "")
	require.NoError(t, err)

	tp, err := r.TracerProvider(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_ = tp.Shutdown(ctx)
	})

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	parent := trace.ContextWithRemoteSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	}))

	_, child := tp.Tracer("test").Start(parent, "child")
	defer child.End()
	assert.True(t, child.SpanContext().IsSampled(), "a sampled parent's child should be sampled even at ratio 0")

	_, root := tp.Tracer("test").Start(context.Background(), "root")
	defer root.End()
	assert.False(t, root.SpanContext().IsSampled(), "a root span should follow the ratio of 0")
}

// TestHTTPCollectorURLAndHeaders proves the wire behaviour of the url and
// headers collector fields: each signal is exported to <url>/v1/<signal>
// with the configured headers attached.
func TestHTTPCollectorURLAndHeaders(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var authHeaders []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		paths = append(paths, req.URL.Path)
		authHeaders = append(authHeaders, req.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	conf := Config{
		Label: "test",
		OpenTelemetryCollector: OTelCollectorConfig{
			HTTP: []Collector{{
				URL:     server.URL + "/otlp",
				Headers: map[string]string{"Authorization": "Basic dGVzdA=="},
			}},
			Metrics: MetricsConfig{PushInterval: "10s"},
		},
	}

	r, err := newResource(conf, "")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tp, err := r.TracerProvider(ctx)
	require.NoError(t, err)
	_, span := tp.Tracer("test").Start(ctx, "test-span")
	span.End()
	require.NoError(t, tp.Shutdown(ctx))

	mp, err := r.MeterProvider(ctx)
	require.NoError(t, err)
	counter, err := mp.Meter("test").Int64Counter("test_counter")
	require.NoError(t, err)
	counter.Add(ctx, 1)
	require.NoError(t, mp.Shutdown(ctx))

	lp, err := r.LoggerProvider(ctx)
	require.NoError(t, err)
	var rec log.Record
	rec.SetBody(attribute.StringValue("hello"))
	lp.Logger("test").Emit(ctx, rec)
	require.NoError(t, lp.Shutdown(ctx))

	mu.Lock()
	defer mu.Unlock()

	assert.ElementsMatch(t, []string{"/otlp/v1/traces", "/otlp/v1/metrics", "/otlp/v1/logs"}, paths)
	for _, h := range authHeaders {
		assert.Equal(t, "Basic dGVzdA==", h)
	}
}

func TestHistogramBucketsViewEmptyIsNoOp(t *testing.T) {
	assert.Nil(t, histogramBucketsView(nil))
}

// TestHistogramBucketsViewAppliesConfiguredBounds proves a configured
// histogram_buckets View, applied as a MeterProvider option the same way
// MeterProvider applies it, overrides the default bucket boundaries for
// every histogram. MeterProvider itself only accepts OTLP readers, so this
// exercises the View-building helper against a ManualReader directly.
func TestHistogramBucketsViewAppliesConfiguredBounds(t *testing.T) {
	buckets := []float64{0.0005, 0.001, 0.0025, 0.005, 0.01}
	view := histogramBucketsView(buckets)
	require.NotNil(t, view)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithView(view))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	hist, err := provider.Meter("test").Float64Histogram("test_timer")
	require.NoError(t, err)
	hist.Record(context.Background(), 0.002)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.Len(t, rm.ScopeMetrics, 1)
	require.Len(t, rm.ScopeMetrics[0].Metrics, 1)

	data, ok := rm.ScopeMetrics[0].Metrics[0].Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, data.DataPoints, 1)
	assert.Equal(t, buckets, data.DataPoints[0].Bounds)
}
