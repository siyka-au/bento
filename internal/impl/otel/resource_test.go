package otel

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/warpstreamlabs/bento/internal/bundle"
	"github.com/warpstreamlabs/bento/internal/component/metrics"
	"github.com/warpstreamlabs/bento/internal/component/tracer"
	"github.com/warpstreamlabs/bento/internal/manager/mock"
	"github.com/warpstreamlabs/bento/internal/telemetry"
)

func newTestRegistry(t *testing.T) *telemetry.Registry {
	t.Helper()
	reg, err := telemetry.NewRegistry([]telemetry.Config{
		{
			Label: "otel",
			OpenTelemetryCollector: telemetry.OTelCollectorConfig{
				GRPC: []telemetry.Collector{{Address: "localhost:4317"}},
				Metrics: telemetry.MetricsConfig{
					PushInterval: "10s",
				},
			},
		},
	}, "")
	require.NoError(t, err)
	return reg
}

func TestTracerResourceNotAvailable(t *testing.T) {
	_, err := bundle.AllTracers.Init(tracer.Config{Type: "resource", Plugin: "otel"}, mock.NewManager())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestTracerResourceOK(t *testing.T) {
	mgr := mock.NewManager()
	mgr.SetGeneric(telemetry.ManagerKey, newTestRegistry(t))

	provider, err := bundle.AllTracers.Init(tracer.Config{Type: "resource", Plugin: "otel"}, mgr)
	require.NoError(t, err)
	require.NotNil(t, provider)

	shutter, ok := provider.(interface{ Shutdown(context.Context) error })
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = shutter.Shutdown(ctx)
}

func TestTracerResourceUnknownLabel(t *testing.T) {
	mgr := mock.NewManager()
	mgr.SetGeneric(telemetry.ManagerKey, newTestRegistry(t))

	_, err := bundle.AllTracers.Init(tracer.Config{Type: "resource", Plugin: "missinglabel"}, mgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missinglabel")
}

func TestMetricsResourceNotAvailable(t *testing.T) {
	_, err := bundle.AllMetrics.Init(metrics.Config{Type: "resource", Plugin: "otel"}, mock.NewManager())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not available")
}

func TestMetricsResourceOK(t *testing.T) {
	mgr := mock.NewManager()
	mgr.SetGeneric(telemetry.ManagerKey, newTestRegistry(t))

	// Not closed: the periodic reader's Shutdown retries against the
	// unreachable collector until our bounded 10s Close timeout, which would
	// needlessly slow down this test (mirrors internal/component/metrics
	// config_test.go, which doesn't close its namespaced metrics either).
	ns, err := bundle.AllMetrics.Init(metrics.Config{Type: "resource", Plugin: "otel"}, mgr)
	require.NoError(t, err)
	require.NotNil(t, ns)
}

func TestMetricsResourceUnknownLabel(t *testing.T) {
	mgr := mock.NewManager()
	mgr.SetGeneric(telemetry.ManagerKey, newTestRegistry(t))

	_, err := bundle.AllMetrics.Init(metrics.Config{Type: "resource", Plugin: "missinglabel"}, mgr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missinglabel")
}

// The following mirror the behavioural tests in metrics_otel_test.go but
// drive metricsCore through the internal metrics.Type interface
// (GetCounterVec/GetGaugeVec/GetTimerVec) rather than the public
// service.MetricsExporter ctor methods, since that's the path the "resource"
// metrics type above uses.

func newTestMetricsCore(t *testing.T) (*metricsCore, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return newMetricsCore(provider), reader
}

func TestMetricsCoreCounterVecSumsPerLabelSet(t *testing.T) {
	core, reader := newTestMetricsCore(t)

	cv := core.GetCounterVec("input_received", "label", "path")
	cv.With("fooinput", "root.input").Incr(2)
	cv.With("fooinput", "root.input").Incr(3)
	cv.With("other", "root.output").Incr(1)

	sum, ok := collect(t, reader)["input_received"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 2)
}

func TestMetricsCoreGaugeRecordsLatestValue(t *testing.T) {
	core, reader := newTestMetricsCore(t)

	g := core.GetGaugeVec("customthing", "topic").With("testtopic")
	g.Set(10)
	g.Set(1234)

	gauge, ok := collect(t, reader)["customthing"].Data.(metricdata.Gauge[int64])
	require.True(t, ok)
	require.Len(t, gauge.DataPoints, 1)
	assert.Equal(t, int64(1234), gauge.DataPoints[0].Value)
}

func TestMetricsCoreTimerRecordsSeconds(t *testing.T) {
	core, reader := newTestMetricsCore(t)

	timer := core.GetTimerVec("input_latency_ns", "path").With("root.input")
	timer.Timing(1_500_000_000)

	hist, ok := collect(t, reader)["input_latency_ns"].Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(t, 1.5, hist.DataPoints[0].Sum)
	assert.Equal(t, uint64(1), hist.DataPoints[0].Count)
}
