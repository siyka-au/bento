package otel

import (
	"context"
	"time"

	runtimeinstr "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// startRuntimeMetrics registers OpenTelemetry's official Go runtime
// instrumentation (go.opentelemetry.io/contrib/instrumentation/runtime) -
// memory, GC and goroutine metrics (go.memory.used, go.memory.limit,
// go.memory.allocated, go.memory.allocations, go.memory.gc.goal,
// go.goroutine.count, go.processor.limit, go.config.gogc) - against
// provider, plus a process_start_time gauge, which that package doesn't
// provide.
//
// runtime.Start only registers an observable callback on the Meter it is
// given; it starts no goroutines or tickers of its own, so there is nothing
// to unregister when provider is shut down - the callback simply stops
// being invoked, and is garbage collected along with the provider.
func startRuntimeMetrics(provider *sdkmetric.MeterProvider) error {
	if err := runtimeinstr.Start(runtimeinstr.WithMeterProvider(provider)); err != nil {
		return err
	}

	startTime := time.Now().Unix()
	meter := provider.Meter("bento")
	_, err := meter.Int64ObservableGauge("process_start_time",
		metric.WithDescription("Unix time (in seconds) at which the process started."),
		metric.WithUnit("s"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(startTime)
			return nil
		}),
	)
	return err
}
