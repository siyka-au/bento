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
	assert.Equal(t, []Collector{{Address: "localhost:4317", Secure: true, Headers: map[string]string{}}}, conf.OpenTelemetryCollector.GRPC)
	assert.Equal(t, []Collector{{Address: "localhost:4318", Secure: false, Headers: map[string]string{}}}, conf.OpenTelemetryCollector.HTTP)
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
	assert.Empty(t, conf.OpenTelemetryCollector.Metrics.HistogramBuckets)
	assert.False(t, conf.OpenTelemetryCollector.Metrics.RuntimeMetrics)
}

func TestFromParsedRuntimeMetricsRoundTrip(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - address: localhost:4317
  metrics:
    runtime_metrics: true
`)

	conf, err := FromParsed(pConf)
	require.NoError(t, err)
	assert.True(t, conf.OpenTelemetryCollector.Metrics.RuntimeMetrics)
}

func TestFromParsedHistogramBucketsRoundTrip(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - address: localhost:4317
  metrics:
    histogram_buckets: [0.0005, 0.001, 0.0025, 0.005, 0.01]
`)

	conf, err := FromParsed(pConf)
	require.NoError(t, err)
	assert.Equal(t, []float64{0.0005, 0.001, 0.0025, 0.005, 0.01}, conf.OpenTelemetryCollector.Metrics.HistogramBuckets)
}

func TestFromParsedHistogramBucketsNonIncreasingError(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - address: localhost:4317
  metrics:
    histogram_buckets: [0.01, 0.01, 0.1]
`)

	_, err := FromParsed(pConf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strictly increasing")
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

func TestFromParsedURLAndHeadersRoundTrip(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - url: https://otel-collector.example.com:4317
      headers:
        Authorization: Basic Z3JwYw==
  http:
    - url: https://otlp-gateway.example.com/otlp
      headers:
        Authorization: Basic aHR0cA==
`)

	conf, err := FromParsed(pConf)
	require.NoError(t, err)

	assert.Equal(t, []Collector{{
		URL:     "https://otel-collector.example.com:4317",
		Headers: map[string]string{"Authorization": "Basic Z3JwYw=="},
	}}, conf.OpenTelemetryCollector.GRPC)
	assert.Equal(t, []Collector{{
		URL:     "https://otlp-gateway.example.com/otlp",
		Headers: map[string]string{"Authorization": "Basic aHR0cA=="},
	}}, conf.OpenTelemetryCollector.HTTP)
}

func TestFromParsedAddressAndURLBothSetError(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  grpc:
    - address: localhost:4317
      url: https://otel-collector.example.com:4317
`)

	_, err := FromParsed(pConf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my_resource")
	assert.Contains(t, err.Error(), "grpc[0]")
	assert.Contains(t, err.Error(), "exactly one of address or url")
}

func TestFromParsedAddressAndURLNeitherSetError(t *testing.T) {
	pConf := parseConfig(t, `
label: my_resource
open_telemetry_collector:
  http:
    - secure: true
`)

	_, err := FromParsed(pConf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "my_resource")
	assert.Contains(t, err.Error(), "http[0]")
	assert.Contains(t, err.Error(), "exactly one of address or url")
}
