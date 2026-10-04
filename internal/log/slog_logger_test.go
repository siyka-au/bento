package log

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
)

func TestLoggerWith(t *testing.T) {
	loggerConfig := NewConfig()
	loggerConfig.AddTimeStamp = false
	loggerConfig.Format = "logfmt"
	loggerConfig.LogLevel = "WARN"
	loggerConfig.StaticFields = map[string]string{
		"@service": "bento_service",
		"@system":  "foo",
	}

	var buf bytes.Buffer

	logger, err := New(&buf, ifs.OS(), loggerConfig)
	require.NoError(t, err)

	logger.Warn("Warning message root module")
	logger.Warn("Warning message root module\n")

	expected := `level=WARN msg="Warning message root module" @service=bento_service @system=foo
level=WARN msg="Warning message root module\n" @service=bento_service @system=foo
`

	assert.Equal(t, expected, buf.String())
}

func TestLoggerWithFields(t *testing.T) {
	loggerConfig := NewConfig()
	loggerConfig.AddTimeStamp = false
	loggerConfig.Format = "logfmt"
	loggerConfig.LogLevel = "WARN"
	loggerConfig.StaticFields = map[string]string{
		"@service": "bento_service",
		"@system":  "foo",
	}

	var buf bytes.Buffer

	logger, err := New(&buf, ifs.OS(), loggerConfig)
	require.NoError(t, err)

	logger = logger.WithFields(map[string]string{
		"foo": "bar", "count": "10", "thing": "is a string", "iscool": "true",
	})

	logger.Warn("Warning message foo fields")

	line := buf.String()
	for _, exp := range []string{
		`level=WARN msg="Warning message foo fields"`,
		"@service=bento_service", "@system=foo",
		"foo=bar", "count=10", `thing="is a string"`, "iscool=true",
	} {
		assert.Contains(t, line, exp)
	}
}

func TestLoggerWithOtherNames(t *testing.T) {
	loggerConfig := NewConfig()
	loggerConfig.AddTimeStamp = false
	loggerConfig.Format = "json"
	loggerConfig.LogLevel = "WARN"
	loggerConfig.StaticFields = map[string]string{
		"@service": "bento_service",
		"@system":  "foo",
	}
	loggerConfig.LevelName = "severity"
	loggerConfig.MessageName = "message"

	var buf bytes.Buffer

	logger, err := New(&buf, ifs.OS(), loggerConfig)
	require.NoError(t, err)

	logger = logger.WithFields(map[string]string{
		"foo": "bar",
	})
	require.NoError(t, err)

	logger.Warn("Warning message foo fields")

	expected := `{"@service":"bento_service","@system":"foo","foo":"bar","message":"Warning message foo fields","severity":"WARN"}
`

	require.JSONEq(t, expected, buf.String())
}

type logCounter struct {
	count int
}

func (l *logCounter) Write(p []byte) (n int, err error) {
	l.count++
	return len(p), nil
}

func TestLogLevels(t *testing.T) {
	for i, lvl := range []string{
		"FATAL",
		"ERROR",
		"WARN",
		"INFO",
		"DEBUG",
		"TRACE",
	} {
		loggerConfig := NewConfig()
		loggerConfig.LogLevel = lvl

		buf := logCounter{}

		logger, err := New(&buf, ifs.OS(), loggerConfig)
		require.NoError(t, err)

		logger.Error("error test")
		logger.Warn("warn test")
		logger.Info("info test")
		logger.Debug("info test")
		logger.Trace("trace test")

		if i != buf.count {
			t.Errorf("Wrong log count for [%v], %v != %v", loggerConfig.LogLevel, i, buf.count)
		}
	}
}

func TestLoggerLevelOff(t *testing.T) {
	for _, lvl := range []string{"OFF", "NONE"} {
		loggerConfig := NewConfig()
		loggerConfig.LogLevel = lvl

		buf := logCounter{}

		logger, err := New(&buf, ifs.OS(), loggerConfig)
		require.NoError(t, err)

		logger.Error("error test")
		logger.Warn("warn test")
		logger.Info("info test")
		logger.Debug("debug test")
		logger.Trace("trace test")

		assert.Equalf(t, 0, buf.count, "level %v should suppress all output", lvl)
	}
}

// TestLoggerFatalExits confirms Fatal still logs the message and terminates
// the process with exit code 1, matching the prior logrus-backed behaviour.
// It re-executes this test binary as a subprocess, since Fatal calls
// os.Exit and cannot be observed in-process.
func TestLoggerFatalExits(t *testing.T) {
	if os.Getenv("BENTO_LOG_FATAL_TEST") == "1" {
		loggerConfig := NewConfig()
		loggerConfig.Format = "logfmt"

		logger, err := New(os.Stdout, ifs.OS(), loggerConfig)
		if err != nil {
			os.Exit(2)
		}
		logger.Fatal("boom")
		os.Exit(3) // unreachable: Fatal must exit the process first
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestLoggerFatalExits")
	cmd.Env = append(os.Environ(), "BENTO_LOG_FATAL_TEST=1")
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.Contains(t, string(out), "boom")
	assert.Contains(t, string(out), "FATAL")
}
