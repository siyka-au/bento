package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestStartRuntimeMetricsRegistersGoroutineCount(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	require.NoError(t, startRuntimeMetrics(provider))

	metrics := collect(t, reader)
	assert.Contains(t, metrics, "go.goroutine.count")
	assert.Contains(t, metrics, "process_start_time")
}
