package log

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
	"github.com/warpstreamlabs/bento/internal/telemetry"
)

// newOpts holds the options configured via NewOpt funcs passed to New.
type newOpts struct {
	telemetryReg *telemetry.Registry
}

// NewOpt is an optional argument to New.
type NewOpt func(*newOpts)

// OptTelemetry provides the telemetry resource registry New resolves
// config.Telemetry against. Without it, a non-empty config.Telemetry is an
// error.
func OptTelemetry(reg *telemetry.Registry) NewOpt {
	return func(o *newOpts) {
		o.telemetryReg = reg
	}
}

// New returns a new logger from a config, or returns an error if the config
// is invalid.
func New(stream io.Writer, fs ifs.FS, config Config, opts ...NewOpt) (Modular, error) {
	var o newOpts
	for _, opt := range opts {
		opt(&o)
	}

	if config.File.Path != "" {
		if config.File.Rotate {
			if config.File.RotateMaxSize < 1 {
				return nil, errors.New("rotate_max_size_mb must be at least 1")
			}
			if config.File.RotateMaxBackups < 0 {
				return nil, errors.New("rotate_max_backups cannot be negative")
			}
			stream = newRotatingWriter(config.File)
		} else {
			fw, err := ifs.OS().OpenFile(config.File.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
			if err == nil {
				var isw bool
				if stream, isw = fw.(io.Writer); !isw {
					err = errors.New("failed to open a writeable file")
				}
			}
			if err != nil {
				return nil, err
			}
		}
	}

	hOpts := &slog.HandlerOptions{
		Level:       parseLevel(config.LogLevel),
		ReplaceAttr: config.replaceAttr,
	}

	var handler slog.Handler
	switch config.Format {
	case "json":
		handler = slog.NewJSONHandler(stream, hOpts)
	case "logfmt":
		handler = slog.NewTextHandler(stream, hOpts)
	default:
		return nil, fmt.Errorf("log format '%v' not recognized", config.Format)
	}

	var telemetryShutdown func(context.Context) error
	if config.Telemetry != "" {
		res, err := o.telemetryReg.Get(config.Telemetry)
		if err != nil {
			return nil, fmt.Errorf("logger telemetry: %w", err)
		}
		provider, err := res.LoggerProvider(context.Background())
		if err != nil {
			return nil, fmt.Errorf("logger telemetry: %w", err)
		}
		telemetryShutdown = provider.Shutdown
		telemetryHandler := otelslog.NewHandler("bento", otelslog.WithLoggerProvider(provider))
		handler = &multiHandler{
			level:    parseLevel(config.LogLevel),
			handlers: []slog.Handler{handler, telemetryHandler},
		}
	}

	logger := slog.New(handler)
	if len(config.StaticFields) > 0 {
		keys := make([]string, 0, len(config.StaticFields))
		for k := range config.StaticFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		attrs := make([]any, 0, len(keys)*2)
		for _, k := range keys {
			attrs = append(attrs, k, config.StaticFields[k])
		}
		logger = logger.With(attrs...)
	}

	adapter := NewBentoLogAdapter(logger)
	adapter.shutdown = telemetryShutdown
	return adapter, nil
}

// replaceAttr applies the configured level/message/timestamp field names and
// gives TRACE/FATAL their literal level names, since slog has no built-in
// concept of either.
func (c Config) replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}

	switch a.Key {
	case slog.TimeKey:
		if !c.AddTimeStamp {
			return slog.Attr{}
		}
		if c.TimestampName != "" {
			a.Key = c.TimestampName
		}
	case slog.MessageKey:
		if c.MessageName != "" {
			a.Key = c.MessageName
		}
	case slog.LevelKey:
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(levelString(lvl))
		}
		if c.LevelName != "" {
			a.Key = c.LevelName
		}
	}
	return a
}

// newRotatingWriter builds a lumberjack rolling writer from a file config.
func newRotatingWriter(f File) *lumberjack.Logger {
	return &lumberjack.Logger{
		Filename:   f.Path,
		MaxSize:    f.RotateMaxSize,
		MaxAge:     f.RotateMaxAge,
		MaxBackups: f.RotateMaxBackups,
		Compress:   f.RotateCompress,
	}
}

//------------------------------------------------------------------------------

// Noop creates and returns a new logger object that writes nothing.
func Noop() Modular {
	return NewBentoLogAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
