package log

import (
	"log/slog"
	"strings"
)

// LevelTrace is a custom slog level for trace-level logging, below
// slog.LevelDebug (-4). Matches zerolog's Trace level semantics, mirroring
// the equivalent constant in github.com/siyka-au/go-ads.
const LevelTrace = slog.Level(-8)

// LevelFatal is a custom slog level for fatal-level logging, above
// slog.LevelError (8). A Fatal log line always terminates the process.
const LevelFatal = slog.Level(12)

// levelOff sits above LevelFatal so that "OFF"/"NONE" suppresses every log
// line, including fatal ones, matching the behaviour of logrus's prior
// PanicLevel-based mapping for the same config values.
const levelOff = slog.Level(16)

// parseLevel maps a logger.level config string onto an slog.Level.
func parseLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "OFF", "NONE":
		return levelOff
	case "FATAL":
		return LevelFatal
	case "ERROR":
		return slog.LevelError
	case "WARN":
		return slog.LevelWarn
	case "INFO":
		return slog.LevelInfo
	case "DEBUG":
		return slog.LevelDebug
	case "TRACE", "ALL":
		return LevelTrace
	default:
		return slog.LevelInfo
	}
}

// levelString renders a level for output, giving TRACE/FATAL their literal
// names since slog's own Level.String() has no notion of either (it would
// otherwise render them as "DEBUG-4" and "ERROR+4").
func levelString(l slog.Level) string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelFatal:
		return "FATAL"
	default:
		return l.String()
	}
}
