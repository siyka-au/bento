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
	return &otelMetrics{provider: provider, meter: provider.Meter("bento")}, reader
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
