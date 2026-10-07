package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/warpstreamlabs/bento/internal/docs"
)

func parseConfig(t *testing.T, yamlStr string) *docs.ParsedConfig {
	t.Helper()

	node, err := docs.UnmarshalYAML([]byte(yamlStr))
	require.NoError(t, err)

	pConf, err := docs.FieldObject("", "").WithChildren(FieldSpecs()...).ParsedConfigFromAny(node)
	require.NoError(t, err)

	return pConf
}

func TestFromParsedRoundTrip(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - address: localhost:4317
      secure: true
  http:
    - address: localhost:4318
  tags:
    env: prod
  traces:
    sampling:
      enabled: true
      ratio: 0.5
  metrics:
    push_interval: 30s
`)

	conf, err := FromParsed(pConf)
	require.NoError(t, err)

	assert.Equal(t, "my_resource", conf.Label)
	assert.Equal(t, []Collector{{Address: "localhost:4317", Secure: true}}, conf.OpenTelemetryCollector.GRPC)
	assert.Equal(t, []Collector{{Address: "localhost:4318", Secure: false}}, conf.OpenTelemetryCollector.HTTP)
	assert.Equal(t, map[string]string{"env": "prod"}, conf.OpenTelemetryCollector.Tags)
	assert.Equal(t, SamplingConfig{Enabled: true, Ratio: 0.5}, conf.OpenTelemetryCollector.Traces.Sampling)
	assert.Equal(t, "30s", conf.OpenTelemetryCollector.Metrics.PushInterval)
}

func TestFromParsedDefaults(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - address: localhost:4317
`)

	conf, err := FromParsed(pConf)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{}, conf.OpenTelemetryCollector.Tags)
	assert.Equal(t, SamplingConfig{Enabled: false, Ratio: 0}, conf.OpenTelemetryCollector.Traces.Sampling)
	assert.Equal(t, "10s", conf.OpenTelemetryCollector.Metrics.PushInterval)
}

func TestFromParsedMissingCollectorsError(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector: {}
`)

	_, err := FromParsed(pConf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my_resource")
	assert.Contains(t, err.Error(), "grpc or http")
}
