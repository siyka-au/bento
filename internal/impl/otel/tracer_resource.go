package otel

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/trace"

	"github.com/warpstreamlabs/bento/internal/bundle"
	"github.com/warpstreamlabs/bento/internal/component/tracer"
	"github.com/warpstreamlabs/bento/internal/docs"
	"github.com/warpstreamlabs/bento/internal/telemetry"
)

// resourceConfigField is the config spec shared by the "resource" tracer and
// metrics types: a single root-level string value naming the
// telemetry_resources entry to export to, e.g. `tracer: { resource: otel }`.
var resourceConfigField = docs.FieldString("", "The label of a telemetry resource, defined in `telemetry_resources`, to export to.")

// telemetryResourceFromManager resolves plugin (the raw value under the
// "resource" type key) to the label of a telemetry_resources entry, and
// looks it up in the *telemetry.Registry that CLI bootstrap stashes on the
// manager via telemetry.ManagerKey.
func telemetryResourceFromManager(nm bundle.NewManagement, plugin any) (*telemetry.Resource, error) {
	pConf, err := resourceConfigField.ParsedConfigFromAny(plugin)
	if err != nil {
		return nil, err
	}
	label, err := pConf.FieldString()
	if err != nil {
		return nil, err
	}

	v, _ := nm.GetGeneric(telemetry.ManagerKey)
	reg, ok := v.(*telemetry.Registry)
	if !ok {
		return nil, errors.New("telemetry resources are not available in this context")
	}
	return reg.Get(label)
}

func init() {
	err := bundle.AllTracers.Add(func(conf tracer.Config, nm bundle.NewManagement) (trace.TracerProvider, error) {
		res, err := telemetryResourceFromManager(nm, conf.Plugin)
		if err != nil {
			return nil, err
		}
		return res.TracerProvider(context.Background())
	}, docs.ComponentSpec{
		Name:    "resource",
		Status:  docs.StatusExperimental,
		Plugin:  true,
		Summary: "Exports traces to a named [telemetry resource](/docs/configuration/resources#telemetry-resources).",
		Description: `
This tracer exports to an OpenTelemetry collector resource defined in the
top level ` + "`telemetry_resources`" + ` field, identified by its label, rather than
configuring its own OTLP export. Per-signal settings, such as trace sampling,
are configured on the telemetry resource itself.`,
		Config: resourceConfigField,
	})
	if err != nil {
		panic(err)
	}
}
