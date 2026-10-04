# Changelog

Changes made in this fork (`siyka-au/bento`) relative to upstream
`warpstreamlabs/bento`. Upstream's own release history is not repeated here, so
sections are titled by topic rather than by version, which avoids clashing with
upstream version numbers when merging `upstream/main`.

This project uses [Conventional Commits](https://www.conventionalcommits.org/)
for commit messages.

## [fork] slog-native logging and OTel log export

Bento's logger was backed by logrus, which predates the standard library's
`log/slog` package. The internal logger is now built on `slog`, so the logging
surface matches `go-ads` and any OpenTelemetry bridge can plug in directly. The
`log.Modular` interface is unchanged, so no component call sites moved.

`TRACE` and `FATAL` are custom `slog` levels, using the same values as go-ads.

A new `logger.otel` block exports every record over OTLP (grpc or http). It is a
sub-block rather than a `type:`/`plugin:` backend like `metrics` and `tracer`,
because there is only ever one process-wide logger. Records go to the primary
output and the collector in the same pass, and both respect `logger.level`.

### Breaking

- **logfmt output changed.** Levels are uppercase (`level=WARN`, not
  `level=warning`), and attributes keep insertion order rather than being sorted
  alphabetically. Anything parsing bento's own text logs must be updated. JSON
  logs keep their existing field names. Prefer `format: json` for parsers.

Before (logrus):

```
level=warning msg="Warning message" @service=bento count=10 foo=bar
```

After (slog):

```
level=WARN msg="Warning message" @service=bento foo=bar count=10
```

### Added

```yaml
logger:
  level: INFO
  format: json
  otel:
    enabled: true
    grpc:
      - address: localhost:4317
    tags:
      deployment: prod
```

## [fork] OTel metrics exporter

Adds an `otel` metrics backend that pushes over OTLP (grpc or http) using the
OpenTelemetry metrics SDK. That SDK was already a transitive dependency but was
not wired to anything. Counters map to Int64Counter, gauges to Int64Gauge, and
timers to a Float64Histogram in seconds, following the OTel convention for
durations.

```yaml
metrics:
  otel:
    grpc:
      - address: localhost:4317
    push_interval: 10s
```

## [fork] OpenTelemetry dependency alignment

The OTel family is pinned to v1.47.0. The gRPC trace exporter had been left
behind at v1.40.0. The logs API and SDK reached v1.47.0 in the same release
train, so logs use stable modules rather than the pre-1.0 ones.

## Phase 2 (planned, not yet implemented): trace instrumentation

Current span coverage is at the input, output, processor, buffer and
branch/workflow boundaries, plus HTTP client and server. Not yet covered:

- cache and rate_limit component access
- a root span per stream lifecycle
- richer per-component span attributes
- reconciling the parallel `internal/tracing` and `internal/tracing/v2` helper
  packages
