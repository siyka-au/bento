package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
)

// connectTimeout bounds exporter construction, mirroring
// internal/impl/otlp/tracer_otlp.go and internal/impl/otel/metrics_otel.go.
const connectTimeout = 30 * time.Second

// Resource is a named OpenTelemetry collector definition. It owns a single
// *resource.Resource (the set of attributes describing this process) that is
// shared across every provider it creates, so traces, metrics and logs all
// report the same service identity.
type Resource struct {
	conf Config
	res  *resource.Resource
}

// newResource builds a Resource from conf. It performs no network I/O;
// exporters (and therefore connections) are only created when a provider
// method is called.
func newResource(conf Config, engineVersion string) (*Resource, error) {
	tags := conf.OpenTelemetryCollector.Tags

	var attrs []attribute.KeyValue
	for k, v := range tags {
		attrs = append(attrs, attribute.String(k, v))
	}
	if _, ok := tags[string(semconv.ServiceNameKey)]; !ok {
		attrs = append(attrs, semconv.ServiceNameKey.String("bento"))
	}
	if _, ok := tags[string(semconv.ServiceVersionKey)]; !ok && engineVersion != "" {
		attrs = append(attrs, semconv.ServiceVersionKey.String(engineVersion))
	}

	return &Resource{
		conf: conf,
		res:  resource.NewWithAttributes(semconv.SchemaURL, attrs...),
	}, nil
}

// Label returns the label this resource is registered under.
func (r *Resource) Label() string {
	return r.conf.Label
}

// OTelResource returns the underlying OpenTelemetry resource shared by every
// provider this Resource creates.
func (r *Resource) OTelResource() *resource.Resource {
	return r.res
}

// TracerProvider builds a new trace provider exporting to every configured
// collector. The caller owns the returned provider and must shut it down.
func (r *Resource) TracerProvider(ctx context.Context) (*tracesdk.TracerProvider, error) {
	opts := []tracesdk.TracerProviderOption{tracesdk.WithResource(r.res)}

	sampling := r.conf.OpenTelemetryCollector.Traces.Sampling
	if sampling.Enabled {
		opts = append(opts, tracesdk.WithSampler(tracesdk.TraceIDRatioBased(sampling.Ratio)))
	}

	for _, c := range r.conf.OpenTelemetryCollector.GRPC {
		exp, err := newTraceGRPCExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, tracesdk.WithBatcher(exp))
	}
	for _, c := range r.conf.OpenTelemetryCollector.HTTP {
		exp, err := newTraceHTTPExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, tracesdk.WithBatcher(exp))
	}

	return tracesdk.NewTracerProvider(opts...), nil
}

func newTraceGRPCExporter(ctx context.Context, c Collector) (tracesdk.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}
	return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
}

func newTraceHTTPExporter(ctx context.Context, c Collector) (tracesdk.SpanExporter, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
}

// MeterProvider builds a new meter provider exporting to every configured
// collector. The caller owns the returned provider and must shut it down.
func (r *Resource) MeterProvider(ctx context.Context) (*sdkmetric.MeterProvider, error) {
	interval, err := time.ParseDuration(r.conf.OpenTelemetryCollector.Metrics.PushInterval)
	if err != nil {
		return nil, err
	}

	opts := []sdkmetric.Option{sdkmetric.WithResource(r.res)}

	for _, c := range r.conf.OpenTelemetryCollector.GRPC {
		exp, err := newMetricGRPCExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval))))
	}
	for _, c := range r.conf.OpenTelemetryCollector.HTTP {
		exp, err := newMetricHTTPExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(interval))))
	}

	return sdkmetric.NewMeterProvider(opts...), nil
}

func newMetricGRPCExporter(ctx context.Context, c Collector) (sdkmetric.Exporter, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}
	return otlpmetricgrpc.New(ctx, opts...)
}

func newMetricHTTPExporter(ctx context.Context, c Collector) (sdkmetric.Exporter, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}
	return otlpmetrichttp.New(ctx, opts...)
}

// LoggerProvider builds a new log provider exporting to every configured
// collector. The caller owns the returned provider and must shut it down.
func (r *Resource) LoggerProvider(ctx context.Context) (*sdklog.LoggerProvider, error) {
	opts := []sdklog.LoggerProviderOption{sdklog.WithResource(r.res)}

	for _, c := range r.conf.OpenTelemetryCollector.GRPC {
		exp, err := newLogGRPCExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
	}
	for _, c := range r.conf.OpenTelemetryCollector.HTTP {
		exp, err := newLogHTTPExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
	}

	return sdklog.NewLoggerProvider(opts...), nil
}

func newLogGRPCExporter(ctx context.Context, c Collector) (sdklog.Exporter, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	return otlploggrpc.New(ctx, opts...)
}

func newLogHTTPExporter(ctx context.Context, c Collector) (sdklog.Exporter, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	return otlploghttp.New(ctx, opts...)
}
