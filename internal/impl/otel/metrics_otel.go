package otel

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"

	"github.com/warpstreamlabs/bento/public/service"
)

func metricsSpec() *service.ConfigSpec {
	return service.NewConfigSpec().
		Summary("Push metrics to an [Open Telemetry collector](https://opentelemetry.io/docs/collector/) over OTLP.").
		Field(service.NewObjectListField("http",
			service.NewStringField("address").
				Description("The endpoint of a collector to send metrics to.").
				Example("localhost:4318"),
			service.NewBoolField("secure").
				Description("Connect to the collector over HTTPS.").
				Default(false),
		).Description("A list of http collectors.").Default([]any{})).
		Field(service.NewObjectListField("grpc",
			service.NewStringField("address").
				Description("The endpoint of a collector to send metrics to.").
				Example("localhost:4317"),
			service.NewBoolField("secure").
				Description("Connect to the collector with client transport security.").
				Default(false),
		).Description("A list of grpc collectors.").Default([]any{})).
		Field(service.NewStringMapField("tags").
			Description("A map of tags to add to all metrics as resource attributes.").
			Default(map[string]any{}).
			Advanced()).
		Field(service.NewDurationField("push_interval").
			Description("The interval between pushes of accumulated metrics to the collector.").
			Default("10s").
			Advanced())
}

func init() {
	err := service.RegisterMetricsExporter("otel", metricsSpec(),
		func(conf *service.ParsedConfig, _ *service.Logger) (service.MetricsExporter, error) {
			return newOtelMetricsFromParsed(conf)
		})
	if err != nil {
		panic(err)
	}
}

type collector struct {
	address string
	secure  bool
}

func collectorsFromParsed(conf *service.ParsedConfig, name string) ([]collector, error) {
	list, err := conf.FieldObjectList(name)
	if err != nil {
		return nil, err
	}
	collectors := make([]collector, 0, len(list))
	for _, pc := range list {
		address, err := pc.FieldString("address")
		if err != nil {
			return nil, err
		}
		secure, err := pc.FieldBool("secure")
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, collector{address: address, secure: secure})
	}
	return collectors, nil
}

func newOtelMetricsFromParsed(conf *service.ParsedConfig) (service.MetricsExporter, error) {
	grpcCollectors, err := collectorsFromParsed(conf, "grpc")
	if err != nil {
		return nil, err
	}
	httpCollectors, err := collectorsFromParsed(conf, "http")
	if err != nil {
		return nil, err
	}
	if len(grpcCollectors)+len(httpCollectors) == 0 {
		return nil, errors.New("at least one grpc or http collector must be specified")
	}

	tags, err := conf.FieldStringMap("tags")
	if err != nil {
		return nil, err
	}
	interval, err := conf.FieldDuration("push_interval")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var exporters []sdkmetric.Reader
	for _, c := range grpcCollectors {
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(c.address)}
		if !c.secure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		exp, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)))
	}
	for _, c := range httpCollectors {
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(c.address)}
		if !c.secure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		exp, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval)))
	}

	var attrs []attribute.KeyValue
	for k, v := range tags {
		attrs = append(attrs, attribute.String(k, v))
	}
	if _, ok := tags[string(semconv.ServiceNameKey)]; !ok {
		attrs = append(attrs, semconv.ServiceNameKey.String("bento"))
	}

	opts := []sdkmetric.Option{
		sdkmetric.WithResource(resource.NewWithAttributes(semconv.SchemaURL, attrs...)),
	}
	for _, r := range exporters {
		opts = append(opts, sdkmetric.WithReader(r))
	}
	provider := sdkmetric.NewMeterProvider(opts...)

	return &otelMetrics{
		provider: provider,
		meter:    provider.Meter("bento"),
	}, nil
}

// otelMetrics adapts bento's metrics model to OpenTelemetry. OTel instruments
// are created once per metric name, and label values are bound as attributes
// at record time. This differs from the prometheus backend, which creates one
// vector per name and then one child per label combination.
type otelMetrics struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
}

func labelAttributes(keys, values []string) []attribute.KeyValue {
	n := min(len(keys), len(values))
	attrs := make([]attribute.KeyValue, 0, n)
	for i := range n {
		attrs = append(attrs, attribute.String(keys[i], values[i]))
	}
	return attrs
}

func (o *otelMetrics) NewCounterCtor(name string, labelKeys ...string) service.MetricsExporterCounterCtor {
	// OTel returns a no-op instrument alongside any creation error, which is an
	// acceptable degradation since the MetricsExporter interface has no error path.
	counter, _ := o.meter.Int64Counter(name)
	return func(labelValues ...string) service.MetricsExporterCounter {
		return &otelCounter{
			counter: counter,
			opts:    []metric.AddOption{metric.WithAttributes(labelAttributes(labelKeys, labelValues)...)},
		}
	}
}

func (o *otelMetrics) NewTimerCtor(name string, labelKeys ...string) service.MetricsExporterTimerCtor {
	timer, _ := o.meter.Float64Histogram(name, metric.WithUnit("s"))
	return func(labelValues ...string) service.MetricsExporterTimer {
		return &otelTimer{
			timer: timer,
			opts:  []metric.RecordOption{metric.WithAttributes(labelAttributes(labelKeys, labelValues)...)},
		}
	}
}

func (o *otelMetrics) NewGaugeCtor(name string, labelKeys ...string) service.MetricsExporterGaugeCtor {
	gauge, _ := o.meter.Int64Gauge(name)
	return func(labelValues ...string) service.MetricsExporterGauge {
		return &otelGauge{
			gauge: gauge,
			opts:  []metric.RecordOption{metric.WithAttributes(labelAttributes(labelKeys, labelValues)...)},
		}
	}
}

func (o *otelMetrics) Close(ctx context.Context) error {
	return o.provider.Shutdown(ctx)
}

type otelCounter struct {
	counter metric.Int64Counter
	opts    []metric.AddOption
}

func (c *otelCounter) Incr(count int64) {
	c.counter.Add(context.Background(), count, c.opts...)
}

type otelTimer struct {
	timer metric.Float64Histogram
	opts  []metric.RecordOption
}

// Timing receives nanoseconds, which are converted to seconds to follow the
// OpenTelemetry semantic convention for durations.
func (t *otelTimer) Timing(deltaNs int64) {
	t.timer.Record(context.Background(), float64(deltaNs)/float64(time.Second), t.opts...)
}

type otelGauge struct {
	gauge metric.Int64Gauge
	opts  []metric.RecordOption
}

func (g *otelGauge) Set(value int64) {
	g.gauge.Record(context.Background(), value, g.opts...)
}
