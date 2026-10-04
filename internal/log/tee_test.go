package log

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/warpstreamlabs/bento/internal/filepath/ifs"
)

func TestLoggerTee(t *testing.T) {
	var bufA, bufB bytes.Buffer

	loggerConfig := NewConfig()
	loggerConfig.AddTimeStamp = false
	loggerConfig.Format = "logfmt"
	loggerConfig.LogLevel = "WARN"
	loggerConfig.StaticFields = map[string]string{
		"@service": "bento_service",
		"@system":  "foo",
	}

	loggerA, err := New(&bufA, ifs.OS(), loggerConfig)
	require.NoError(t, err)

	loggerConfig = NewConfig()
	loggerConfig.AddTimeStamp = false
	loggerConfig.Format = "logfmt"
	loggerConfig.LogLevel = "DEBUG"
	loggerConfig.StaticFields = map[string]string{
		"@service": "bento_service",
		"@system":  "bar",
	}

	loggerB, err := New(&bufB, ifs.OS(), loggerConfig)
	require.NoError(t, err)

	logger := TeeLogger(loggerA, loggerB)

	logger.Warn("Warning message root module")
	logger.Debug("Debug log root module")

	logger2 := logger.WithFields(map[string]string{
		"foo": "bar", "count": "10", "thing": "is a string", "iscool": "true",
	})
	require.NoError(t, err)
	logger2.Debug("Debug log foo fields")
	logger2.Warn("Warning message foo fields")

	logger.Warn("Warning message root module\n")
	logger.Debug("Debug message root again")

	// WithFields takes a map, so the order of the extra fields it adds is
	// not guaranteed; assert line-by-line and check extra fields via Contains.
	linesA := strings.Split(strings.TrimRight(bufA.String(), "\n"), "\n")
	require.Len(t, linesA, 3)
	assert.Equal(t, `level=WARN msg="Warning message root module" @service=bento_service @system=foo`, linesA[0])
	for _, exp := range []string{
		`level=WARN msg="Warning message foo fields" @service=bento_service @system=foo`,
		"count=10", "foo=bar", "iscool=true", `thing="is a string"`,
	} {
		assert.Contains(t, linesA[1], exp)
	}
	assert.Equal(t, `level=WARN msg="Warning message root module\n" @service=bento_service @system=foo`, linesA[2])

	linesB := strings.Split(strings.TrimRight(bufB.String(), "\n"), "\n")
	require.Len(t, linesB, 6)
	assert.Equal(t, `level=WARN msg="Warning message root module" @service=bento_service @system=bar`, linesB[0])
	assert.Equal(t, `level=DEBUG msg="Debug log root module" @service=bento_service @system=bar`, linesB[1])
	for _, exp := range []string{
		`level=DEBUG msg="Debug log foo fields" @service=bento_service @system=bar`,
		"count=10", "foo=bar", "iscool=true", `thing="is a string"`,
	} {
		assert.Contains(t, linesB[2], exp)
	}
	for _, exp := range []string{
		`level=WARN msg="Warning message foo fields" @service=bento_service @system=bar`,
		"count=10", "foo=bar", "iscool=true", `thing="is a string"`,
	} {
		assert.Contains(t, linesB[3], exp)
	}
	assert.Equal(t, `level=WARN msg="Warning message root module\n" @service=bento_service @system=bar`, linesB[4])
	assert.Equal(t, `level=DEBUG msg="Debug message root again" @service=bento_service @system=bar`, linesB[5])
}
