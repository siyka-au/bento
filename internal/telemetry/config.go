// Package telemetry defines named OpenTelemetry collector resources
// ("telemetry resources") that the logger, metrics and tracer components can
// reference by label, rather than each configuring its own OTLP export.
//
// This package is a leaf: it must not import any other internal/ or
// public/ bento package (besides internal/docs), so that internal/log,
// internal/manager and internal/impl/* can all import it without creating
// import cycles.
package telemetry

import (
	"fmt"

	"github.com/warpstreamlabs/bento/internal/docs"
)

const (
	fieldLabel                  = "label"
	fieldOpenTelemetryCollector = "open_telemetry_collector"
	fieldGRPC                   = "grpc"
	fieldHTTP                   = "http"
	fieldAddress                = "address"
	fieldURL                    = "url"
	fieldSecure                 = "secure"
	fieldHeaders                = "headers"
	fieldTags                   = "tags"
	fieldTraces                 = "traces"
	fieldSampling               = "sampling"
	fieldSamplingEnabled        = "enabled"
	fieldSamplingRatio          = "ratio"
	fieldMetrics                = "metrics"
	fieldPushInterval           = "push_interval"
)

// Collector describes a single OTLP collector endpoint to export telemetry
// to. Exactly one of Address or URL must be set.
type Collector struct {
	Address string            `yaml:"address"`
	URL     string            `yaml:"url"`
	Secure  bool              `yaml:"secure"`
	Headers map[string]string `yaml:"headers"`
}

// SamplingConfig contains configuration for trace sampling.
type SamplingConfig struct {
	Enabled bool    `yaml:"enabled"`
	Ratio   float64 `yaml:"ratio"`
}

// TracesConfig contains configuration specific to exported traces.
type TracesConfig struct {
	Sampling SamplingConfig `yaml:"sampling"`
}

// MetricsConfig contains configuration specific to exported metrics.
type MetricsConfig struct {
	PushInterval string `yaml:"push_interval"`
}

// OTelCollectorConfig contains configuration for exporting telemetry to one
// or more OpenTelemetry collectors via OTLP.
type OTelCollectorConfig struct {
	GRPC    []Collector       `yaml:"grpc"`
	HTTP    []Collector       `yaml:"http"`
	Tags    map[string]string `yaml:"tags"`
	Traces  TracesConfig      `yaml:"traces"`
	Metrics MetricsConfig     `yaml:"metrics"`
}

// Config describes a single named telemetry resource.
type Config struct {
	Label                  string              `yaml:"label"`
	OpenTelemetryCollector OTelCollectorConfig `yaml:"open_telemetry_collector"`
}

// FieldSpecs returns the field specs for a single telemetry resource entry.
func FieldSpecs() docs.FieldSpecs {
	return docs.FieldSpecs{
		docs.FieldString(fieldLabel, "The label of this telemetry resource, referenced by the logger, metrics and tracer components that want to export to it."),
		docs.FieldObject(fieldOpenTelemetryCollector, "Defines one or more [Open Telemetry collectors](https://opentelemetry.io/docs/collector/) to export telemetry to. Like any OpenTelemetry SDK exporter, a collector setting left unset here may be picked up instead from the standard `OTEL_EXPORTER_OTLP_*` environment variables.").WithChildren(
			docs.FieldObject(fieldGRPC, "A collector to export telemetry to over gRPC. Exactly one of `address` or `url` must be set.").WithChildren(
				docs.FieldString(fieldAddress, "The endpoint of a collector to export telemetry to.", "localhost:4317").Optional(),
				docs.FieldString(fieldURL, "An alternative to `address` giving the full endpoint URL of a collector, including scheme. TLS follows the URL scheme (`https` enables it, `http` disables it), so `secure` is ignored when `url` is set. Any path component is ignored, since gRPC has no use for one.", "https://otel-collector.example.com:4317").Optional(),
				docs.FieldBool(fieldSecure, "Connect to the collector with client transport security.").HasDefault(false),
				docs.FieldString(fieldHeaders, "Extra headers to send with every export request, such as an `Authorization` header for a hosted backend. Values are usually populated from environment variables, e.g. `${GRAFANA_CLOUD_OTLP_AUTH}`.").Map().HasDefault(map[string]any{}),
			).Array().HasDefault([]any{}),
			docs.FieldObject(fieldHTTP, "A collector to export telemetry to over HTTP. Exactly one of `address` or `url` must be set.").WithChildren(
				docs.FieldString(fieldAddress, "The endpoint of a collector to export telemetry to.", "localhost:4318").Optional(),
				docs.FieldString(fieldURL, "An alternative to `address` giving the base URL of a collector, such as a hosted OTLP backend. Each signal is sent to this URL with `/v1/traces`, `/v1/metrics` or `/v1/logs` appended. TLS follows the URL scheme (`https` enables it, `http` disables it), so `secure` is ignored when `url` is set.", "https://otlp-gateway.example.com/otlp").Optional(),
				docs.FieldBool(fieldSecure, "Connect to the collector over HTTPS.").HasDefault(false),
				docs.FieldString(fieldHeaders, "Extra headers to send with every export request, such as an `Authorization` header for a hosted backend. Values are usually populated from environment variables, e.g. `${GRAFANA_CLOUD_OTLP_AUTH}`.").Map().HasDefault(map[string]any{}),
			).Array().HasDefault([]any{}),
			docs.FieldString(fieldTags, "A map of tags to add to all exported telemetry as resource attributes.").Map().HasDefault(map[string]any{}),
			docs.FieldObject(fieldTraces, "Settings specific to exported traces.").WithChildren(
				docs.FieldObject(fieldSampling, "Settings for trace sampling. Sampling is recommended for high-volume production workloads.").WithChildren(
					docs.FieldBool(fieldSamplingEnabled, "Whether to enable sampling.").HasDefault(false),
					docs.FieldFloat(fieldSamplingRatio, "Sets the ratio of traces to sample.").Optional(),
				),
			),
			docs.FieldObject(fieldMetrics, "Settings specific to exported metrics.").WithChildren(
				docs.FieldString(fieldPushInterval, "The interval between pushes of accumulated metrics to the collector.").HasDefault("10s"),
			),
		),
	}
}

// FromParsed parses a single telemetry resource entry from a parsed config.
func FromParsed(p *docs.ParsedConfig) (conf Config, err error) {
	if conf.Label, err = p.FieldString(fieldLabel); err != nil {
		return
	}

	cConf := p.Namespace(fieldOpenTelemetryCollector)
	if conf.OpenTelemetryCollector, err = otelCollectorConfigFromParsed(cConf, conf.Label); err != nil {
		return
	}

	if len(conf.OpenTelemetryCollector.GRPC) == 0 && len(conf.OpenTelemetryCollector.HTTP) == 0 {
		err = fmt.Errorf("telemetry resource %q: at least one grpc or http collector must be specified", conf.Label)
		return
	}

	return
}

func otelCollectorConfigFromParsed(p *docs.ParsedConfig, label string) (conf OTelCollectorConfig, err error) {
	if conf.GRPC, err = collectorsFromParsed(p, fieldGRPC, label); err != nil {
		return
	}
	if conf.HTTP, err = collectorsFromParsed(p, fieldHTTP, label); err != nil {
		return
	}
	if conf.Tags, err = p.FieldStringMap(fieldTags); err != nil {
		return
	}

	if p.Contains(fieldTraces) {
		if conf.Traces, err = tracesConfigFromParsed(p.Namespace(fieldTraces)); err != nil {
			return
		}
	}

	if p.Contains(fieldMetrics) {
		if conf.Metrics, err = metricsConfigFromParsed(p.Namespace(fieldMetrics)); err != nil {
			return
		}
	}

	return
}

func collectorsFromParsed(p *docs.ParsedConfig, field, label string) ([]Collector, error) {
	list, err := p.FieldObjectList(field)
	if err != nil {
		return nil, err
	}
	collectors := make([]Collector, 0, len(list))
	for i, c := range list {
		var address, url string
		if c.Contains(fieldAddress) {
			if address, err = c.FieldString(fieldAddress); err != nil {
				return nil, err
			}
		}
		if c.Contains(fieldURL) {
			if url, err = c.FieldString(fieldURL); err != nil {
				return nil, err
			}
		}
		if (address == "") == (url == "") {
			return nil, fmt.Errorf("telemetry resource %q: %s[%d]: exactly one of address or url must be set", label, field, i)
		}

		secure, err := c.FieldBool(fieldSecure)
		if err != nil {
			return nil, err
		}
		headers, err := c.FieldStringMap(fieldHeaders)
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, Collector{Address: address, URL: url, Secure: secure, Headers: headers})
	}
	return collectors, nil
}

func tracesConfigFromParsed(p *docs.ParsedConfig) (conf TracesConfig, err error) {
	if !p.Contains(fieldSampling) {
		return
	}
	sConf := p.Namespace(fieldSampling)
	if conf.Sampling.Enabled, err = sConf.FieldBool(fieldSamplingEnabled); err != nil {
		return
	}
	if sConf.Contains(fieldSamplingRatio) {
		if conf.Sampling.Ratio, err = sConf.FieldFloat(fieldSamplingRatio); err != nil {
			return
		}
	}
	return
}

func metricsConfigFromParsed(p *docs.ParsedConfig) (conf MetricsConfig, err error) {
	if conf.PushInterval, err = p.FieldString(fieldPushInterval); err != nil {
		return
	}
	return
}
