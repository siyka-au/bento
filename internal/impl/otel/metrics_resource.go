package otel

import (
	"context"

	"github.com/warpstreamlabs/bento/internal/bundle"
	"github.com/warpstreamlabs/bento/internal/component/metrics"
	"github.com/warpstreamlabs/bento/internal/docs"
)

func init() {
	err := bundle.AllMetrics.Add(func(conf metrics.Config, nm bundle.NewManagement) (metrics.Type, error) {
		res, err := telemetryResourceFromManager(nm, conf.Plugin)
		if err != nil {
			return nil, err
		}
		provider, err := res.MeterProvider(context.Background())
		if err != nil {
			return nil, err
		}
		return newMetricsCore(provider), nil
	}, docs.ComponentSpec{
		Name:    "resource",
		Status:  docs.StatusExperimental,
		Plugin:  true,
		Summary: "Exports metrics to a named [telemetry resource](/docs/configuration/resources#telemetry-resources).",
		Description: `
This metrics type exports to an OpenTelemetry collector resource defined in
the top level ` + "`telemetry_resources`" + ` field, identified by its label, rather
than configuring its own OTLP export. Per-signal settings, such as the
metrics push interval, are configured on the telemetry resource itself.`,
		Config: resourceConfigField,
	})
	if err != nil {
		panic(err)
	}
}
