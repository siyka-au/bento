package log

import "context"

// Shutdown flushes and releases any resources held by l, if it has any to
// release. This is primarily relevant for loggers exporting to an
// OpenTelemetry collector, whose batched records would otherwise be lost on
// process exit.
func Shutdown(ctx context.Context, l Modular) error {
	s, ok := l.(interface{ Shutdown(context.Context) error })
	if !ok {
		return nil
	}
	return s.Shutdown(ctx)
}
