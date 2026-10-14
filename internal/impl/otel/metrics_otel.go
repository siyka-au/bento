package otel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"

	"github.com/warpstreamlabs/bento/internal/component/metrics"
	"github.com/warpstreamlabs/bento/public/service"
)

// closeTimeout bounds shutdown of the OTel meter provider when the caller
// gives us no context of their own to work with (metrics.Type.Close takes
// none).
const closeTimeout = 10 * time.Second

// defaultHistogramBuckets are the bucket boundaries (in seconds) applied to
// timer histograms when histogram_buckets is left empty. They match
// prometheus.DefBuckets, for parity with Bento's "prometheus" metrics
// exporter (internal/impl/prometheus/metrics_prometheus.go), and replace the
// OTel SDK's own default boundaries, which are scaled for milliseconds and
// put every real latency in bucket zero.
var defaultHistogramBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

// histogramBucketsView validates a configured list of histogram bucket
// boundaries (in seconds) and, if non-empty, returns an SDK option that
// overrides the default aggregation for every histogram instrument - there
// being only one in this package: the timer. An empty list returns a nil
// option, leaving the instrument-level default (defaultHistogramBuckets) in
// place.
func histogramBucketsView(buckets []float64) (sdkmetric.Option, error) {
	if len(buckets) == 0 {
		return nil, nil
	}
	for i := 1; i < len(buckets); i++ {
		if buckets[i] <= buckets[i-1] {
			return nil, fmt.Errorf("histogram_buckets must be strictly increasing: %v", buckets)
		}
	}
	view := sdkmetric.NewView(
		sdkmetric.Instrument{Kind: sdkmetric.InstrumentKindHistogram},
		sdkmetric.Stream{Aggregation: sdkmetric.AggregationExplicitBucketHistogram{Boundaries: buckets}},
	)
	return sdkmetric.WithView(view), nil
}

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
			Advanced()).
		Field(service.NewFloatListField("histogram_buckets").
			Description("Timing metrics histogram buckets, in seconds. If left empty, defaults to the same buckets as Bento's `prometheus` metrics exporter (`[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`). The smallest default bucket is 5ms, so fast pipelines may want finer buckets, e.g. `[0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1]`.").
			Advanced().
			Default([]any{}))
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
	histogramBuckets, err := conf.FieldFloatList("histogram_buckets")
	if err != nil {
		return nil, err
	}
	bucketsOpt, err := histogramBucketsView(histogramBuckets)
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
	if bucketsOpt != nil {
		opts = append(opts, bucketsOpt)
	}
	for _, r := range exporters {
		opts = append(opts, sdkmetric.WithReader(r))
	}
	provider := sdkmetric.NewMeterProvider(opts...)

	return &otelMetrics{core: newMetricsCore(provider)}, nil
}

// metricsCore adapts bento's internal metrics.Type model to OpenTelemetry,
// and is shared by the "otel" push metrics exporter (public plugin API,
// below) and the "resource" metrics type (internal/impl/otel/metrics_resource.go),
// which exports to a named telemetry_resources entry instead of its own
// collector config. OTel instruments are created once per metric name, and
// label values are bound as attributes at record time. This differs from the
// prometheus backend, which creates one vector per name and then one child
// per label combination.
type metricsCore struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter
}

func newMetricsCore(provider *sdkmetric.MeterProvider) *metricsCore {
	return &metricsCore{provider: provider, meter: provider.Meter("bento")}
}

func labelAttributes(keys, values []string) []attribute.KeyValue {
	n := min(len(keys), len(values))
	attrs := make([]attribute.KeyValue, 0, n)
	for i := range n {
		attrs = append(attrs, attribute.String(keys[i], values[i]))
	}
	return attrs
}

func (o *metricsCore) GetCounter(path string) metrics.StatCounter {
	return o.GetCounterVec(path).With()
}

func (o *metricsCore) GetCounterVec(path string, labelNames ...string) metrics.StatCounterVec {
	// OTel returns a no-op instrument alongside any creation error, which is an
	// acceptable degradation since metrics.Type has no error path either.
	counter, _ := o.meter.Int64Counter(path)
	return &otelCounterVec{counter: counter, labelKeys: labelNames}
}

func (o *metricsCore) GetTimer(path string) metrics.StatTimer {
	return o.GetTimerVec(path).With()
}

func (o *metricsCore) GetTimerVec(path string, labelNames ...string) metrics.StatTimerVec {
	// The explicit boundaries set here are only the instrument default: a
	// configured histogram_buckets View (see histogramBucketsView) takes
	// precedence over it for every histogram, regardless of which exporter
	// built the provider.
	timer, _ := o.meter.Float64Histogram(path,
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(defaultHistogramBuckets...),
	)
	return &otelTimerVec{timer: timer, labelKeys: labelNames}
}

func (o *metricsCore) GetGauge(path string) metrics.StatGauge {
	return o.GetGaugeVec(path).With()
}

func (o *metricsCore) GetGaugeVec(path string, labelNames ...string) metrics.StatGaugeVec {
	gauge, _ := o.meter.Int64Gauge(path)
	return &otelGaugeVec{gauge: gauge, labelKeys: labelNames}
}

// HandlerFunc returns nil, as OTel metrics are pushed to a collector rather
// than scraped over HTTP.
func (o *metricsCore) HandlerFunc() http.HandlerFunc {
	return nil
}

func (o *metricsCore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	return o.provider.Shutdown(ctx)
}

// otelMetrics adapts metricsCore to the public service.MetricsExporter
// plugin interface used by the "otel" push metrics exporter registered
// above.
type otelMetrics struct {
	core *metricsCore
}

func (o *otelMetrics) NewCounterCtor(name string, labelKeys ...string) service.MetricsExporterCounterCtor {
	cv := o.core.GetCounterVec(name, labelKeys...)
	return func(labelValues ...string) service.MetricsExporterCounter {
		return cv.With(labelValues...)
	}
}

func (o *otelMetrics) NewTimerCtor(name string, labelKeys ...string) service.MetricsExporterTimerCtor {
	tv := o.core.GetTimerVec(name, labelKeys...)
	return func(labelValues ...string) service.MetricsExporterTimer {
		return tv.With(labelValues...)
	}
}

func (o *otelMetrics) NewGaugeCtor(name string, labelKeys ...string) service.MetricsExporterGaugeCtor {
	gv := o.core.GetGaugeVec(name, labelKeys...)
	return func(labelValues ...string) service.MetricsExporterGauge {
		return gv.With(labelValues...)
	}
}

func (o *otelMetrics) Close(ctx context.Context) error {
	return o.core.provider.Shutdown(ctx)
}

//------------------------------------------------------------------------------

type otelCounterVec struct {
	counter   metric.Int64Counter
	labelKeys []string
}

func (v *otelCounterVec) With(labelValues ...string) metrics.StatCounter {
	opts := []metric.AddOption{metric.WithAttributes(labelAttributes(v.labelKeys, labelValues)...)}
	// The OTel SDK only exports a series for an attribute set once Add has
	// been called for it, so without this, a counter like output_error first
	// appears in Prometheus at its first increment, and increase()/rate()
	// treat that as the baseline and report 0 across the gap. Add(0) is
	// idempotent in value, so binding the same label set again (e.g. a second
	// GetCounter call for the same path) is harmless.
	v.counter.Add(context.Background(), 0, opts...)
	return &otelCounter{
		counter: v.counter,
		opts:    opts,
	}
}

type otelTimerVec struct {
	timer     metric.Float64Histogram
	labelKeys []string
}

func (v *otelTimerVec) With(labelValues ...string) metrics.StatTimer {
	return &otelTimer{
		timer: v.timer,
		opts:  []metric.RecordOption{metric.WithAttributes(labelAttributes(v.labelKeys, labelValues)...)},
	}
}

type otelGaugeVec struct {
	gauge     metric.Int64Gauge
	labelKeys []string
}

func (v *otelGaugeVec) With(labelValues ...string) metrics.StatGauge {
	return &otelGauge{
		gauge: v.gauge,
		opts:  []metric.RecordOption{metric.WithAttributes(labelAttributes(v.labelKeys, labelValues)...)},
	}
}

//------------------------------------------------------------------------------

type otelCounter struct {
	counter metric.Int64Counter
	opts    []metric.AddOption
}

func (c *otelCounter) Incr(count int64) {
	c.counter.Add(context.Background(), count, c.opts...)
}

// IncrFloat64 truncates to an integer amount, since the underlying instrument
// is an Int64Counter (mirroring the fallback behaviour of airGapCounter in
// public/service/metrics.go for exporters that don't support float deltas).
func (c *otelCounter) IncrFloat64(count float64) {
	c.Incr(int64(count))
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
	// v tracks the current value so that Incr/Decr can be applied on top of
	// it, since Int64Gauge only exposes an absolute Record.
	v atomic.Int64
}

func (g *otelGauge) Set(value int64) {
	g.v.Store(value)
	g.gauge.Record(context.Background(), value, g.opts...)
}

func (g *otelGauge) Incr(count int64) {
	g.Set(g.v.Add(count))
}

func (g *otelGauge) Decr(count int64) {
	g.Set(g.v.Add(-count))
}

func (g *otelGauge) SetFloat64(value float64) {
	g.Set(int64(value))
}

func (g *otelGauge) IncrFloat64(count float64) {
	g.Incr(int64(count))
}

func (g *otelGauge) DecrFloat64(count float64) {
	g.Decr(int64(count))
}
