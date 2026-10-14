package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newTestMetrics(t *testing.T) (*otelMetrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return newOtelMetrics(provider), reader
}

func collect(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func TestOtelCounterBindsLabelsAtRecordTime(t *testing.T) {
	m, reader := newTestMetrics(t)

	ctor := m.NewCounterCtor("input_received", "label", "path")
	ctor("fooinput", "root.input").Incr(2)
	ctor("fooinput", "root.input").Incr(3)
	ctor("other", "root.output").Incr(1)

	sum, ok := collect(t, reader)["input_received"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 2)

	byLabel := map[string]int64{}
	for _, dp := range sum.DataPoints {
		v, _ := dp.Attributes.Value(attribute.Key("label"))
		byLabel[v.AsString()] = dp.Value
	}
	assert.Equal(t, int64(5), byLabel["fooinput"])
	assert.Equal(t, int64(1), byLabel["other"])
}

// TestOtelCounterStartsAtZero proves that a counter's series exists as soon
// as its label set is bound, before any increment: the OTel SDK otherwise
// only exports a series for an attribute set once Add has been called for
// it, which makes a counter like output_connection_lost first appear in
// Prometheus at its first increment, and increase()/rate() treat that first
// sample as the baseline and report 0 across the very outage it was meant to
// catch.
func TestOtelCounterStartsAtZero(t *testing.T) {
	m, reader := newTestMetrics(t)

	m.NewCounterCtor("output_connection_lost", "label")("foooutput")

	sum, ok := collect(t, reader)["output_connection_lost"].Data.(metricdata.Sum[int64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(0), sum.DataPoints[0].Value)
}

func TestOtelGaugeSetsLatestValue(t *testing.T) {
	m, reader := newTestMetrics(t)

	g := m.NewGaugeCtor("customthing", "topic")("testtopic")
	g.Set(10)
	g.Set(1234)

	gauge, ok := collect(t, reader)["customthing"].Data.(metricdata.Gauge[int64])
	require.True(t, ok)
	require.Len(t, gauge.DataPoints, 1)
	assert.Equal(t, int64(1234), gauge.DataPoints[0].Value)
}

func TestOtelTimerRecordsSeconds(t *testing.T) {
	m, reader := newTestMetrics(t)

	timer := m.NewTimerCtor("input_latency_ns", "path")("root.input")
	timer.Timing(1_500_000_000)

	hist, ok := collect(t, reader)["input_latency_ns"].Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(t, 1.5, hist.DataPoints[0].Sum)
	assert.Equal(t, uint64(1), hist.DataPoints[0].Count)
}

// TestOtelTimerDefaultBucketsAreSecondScale proves that without a configured
// histogram_buckets View, the default bucket boundaries are
// prometheus.DefBuckets (seconds), not the OTel SDK's own millisecond-scaled
// defaults, which would put every real latency in the first bucket and make
// latency quantiles meaningless. A ~4ms latency should fall into the first
// bucket, (-inf, 0.005].
func TestOtelTimerDefaultBucketsAreSecondScale(t *testing.T) {
	m, reader := newTestMetrics(t)

	timer := m.NewTimerCtor("request_latency_ns")()
	timer.Timing(4_000_000) // 4ms

	hist, ok := collect(t, reader)["request_latency_ns"].Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)

	dp := hist.DataPoints[0]
	assert.Equal(t, defaultHistogramBuckets, dp.Bounds)
	require.NotEmpty(t, dp.BucketCounts)
	assert.Equal(t, uint64(1), dp.BucketCounts[0])
	for _, c := range dp.BucketCounts[1:] {
		assert.Zero(t, c)
	}
}

// TestNewOtelMetricsRuntimeMetricsEnabled proves runtime_metrics flows from
// the exporter's own config through to starting Go runtime instrumentation,
// without erroring.
func TestNewOtelMetricsRuntimeMetricsEnabled(t *testing.T) {
	conf, err := metricsSpec().ParseYAML(`
grpc:
  - address: localhost:4317
runtime_metrics: true
`, nil)
	require.NoError(t, err)

	exp, err := newOtelMetricsFromParsed(conf)
	require.NoError(t, err)
	require.NotNil(t, exp)
}

func TestHistogramBucketsViewEmptyIsNoOp(t *testing.T) {
	opt, err := histogramBucketsView(nil)
	require.NoError(t, err)
	assert.Nil(t, opt)
}

func TestHistogramBucketsViewNonIncreasingErrors(t *testing.T) {
	_, err := histogramBucketsView([]float64{0.1, 0.1, 0.5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strictly increasing")
}

// TestHistogramBucketsViewOverridesDefault proves a configured
// histogram_buckets View takes precedence over the instrument-level default
// set by NewTimerCtor.
func TestHistogramBucketsViewOverridesDefault(t *testing.T) {
	buckets := []float64{0.001, 0.005, 0.01}
	opt, err := histogramBucketsView(buckets)
	require.NoError(t, err)
	require.NotNil(t, opt)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), opt)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	m := newOtelMetrics(provider)
	timer := m.NewTimerCtor("custom_latency_ns")()
	timer.Timing(2_000_000) // 2ms

	hist, ok := collect(t, reader)["custom_latency_ns"].Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	assert.Equal(t, buckets, hist.DataPoints[0].Bounds)
}
