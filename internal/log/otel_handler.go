package log

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
)

// newOtelHandler builds an slog.Handler that exports log records to an
// OpenTelemetry collector via OTLP, using the official slog bridge
// (go.opentelemetry.io/contrib/bridges/otelslog) over an SDK LoggerProvider.
// Collector wiring (grpc/http, secure, 30s connect timeout) mirrors
// internal/impl/otlp/tracer_otlp.go, which solves the same problem for spans.
func newOtelHandler(conf OtelConfig) (slog.Handler, error) {
	ctx := context.Background()

	var opts []sdklog.LoggerProviderOption

	for _, c := range conf.GRPC {
		exp, err := newOtelLogGRPCExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
	}
	for _, c := range conf.HTTP {
		exp, err := newOtelLogHTTPExporter(ctx, c)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdklog.WithProcessor(sdklog.NewBatchProcessor(exp)))
	}

	var attrs []attribute.KeyValue
	for k, v := range conf.Tags {
		attrs = append(attrs, attribute.String(k, v))
	}
	if _, ok := conf.Tags[string(semconv.ServiceNameKey)]; !ok {
		attrs = append(attrs, semconv.ServiceNameKey.String("bento"))
	}
	opts = append(opts, sdklog.WithResource(resource.NewWithAttributes(semconv.SchemaURL, attrs...)))

	provider := sdklog.NewLoggerProvider(opts...)

	return otelslog.NewHandler("bento", otelslog.WithLoggerProvider(provider)), nil
}

func newOtelLogGRPCExporter(ctx context.Context, c OtelCollector) (sdklog.Exporter, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlploggrpc.WithInsecure())
	}
	return otlploggrpc.New(ctx, opts...)
}

func newOtelLogHTTPExporter(ctx context.Context, c OtelCollector) (sdklog.Exporter, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(c.Address)}
	if !c.Secure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	return otlploghttp.New(ctx, opts...)
}
