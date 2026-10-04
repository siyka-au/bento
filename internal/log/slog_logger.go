package log

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
)

// New returns a new logger from a config, or returns an error if the config
// is invalid.
func New(stream io.Writer, fs ifs.FS, config Config) (Modular, error) {
	if config.File.Path != "" {
		if config.File.Rotate {
			stream = &lumberjack.Logger{
				Filename:   config.File.Path,
				MaxSize:    10,
				MaxAge:     config.File.RotateMaxAge,
				MaxBackups: 1,
				Compress:   true,
			}
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

	opts := &slog.HandlerOptions{
		Level:       parseLevel(config.LogLevel),
		ReplaceAttr: config.replaceAttr,
	}

	var handler slog.Handler
	switch config.Format {
	case "json":
		handler = slog.NewJSONHandler(stream, opts)
	case "logfmt":
		handler = slog.NewTextHandler(stream, opts)
	default:
		return nil, fmt.Errorf("log format '%v' not recognized", config.Format)
	}

	if config.Otel.Enabled {
		otelHandler, err := newOtelHandler(config.Otel)
		if err != nil {
			return nil, err
		}
		handler = &multiHandler{
			level:    parseLevel(config.LogLevel),
			handlers: []slog.Handler{handler, otelHandler},
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

	return NewBentoLogAdapter(logger), nil
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

//------------------------------------------------------------------------------

// Noop creates and returns a new logger object that writes nothing.
func Noop() Modular {
	return NewBentoLogAdapter(slog.New(slog.NewTextHandler(io.Discard, nil)))
}
