package common_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"

	"github.com/warpstreamlabs/bento/internal/cli/common"

	_ "github.com/warpstreamlabs/bento/public/components/pure"
)

// TestRunServiceErrWriter proves that an error raised before the service
// logger exists (here: a config file that fails to load) is written to the
// CLIOpts.ErrWriter rather than being hard-wired to os.Stderr, so that a
// program embedding the CLI (via public/service.RunCLI and
// CLIOptSetErrWriter) can capture it.
//
// RunService is exercised directly, rather than through the full CLI App,
// because the App's own Action wraps RunService's non-zero return code in
// an os.Exit call, which would make in-process testing of this path
// impossible.
func TestRunServiceErrWriter(t *testing.T) {
	cliOpts := common.NewCLIOpts("1.2.3", "aaa")

	var errBuf bytes.Buffer
	cliOpts.ErrWriter = &errBuf

	// A path that does not exist, so config loading fails before the
	// logger is created.
	badConfigPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	app := &cli.App{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}},
			&cli.StringSliceFlag{Name: "set", Aliases: []string{"s"}},
			&cli.StringSliceFlag{Name: "resources", Aliases: []string{"r"}},
			&cli.BoolFlag{Name: "chilled"},
			&cli.BoolFlag{Name: "watcher", Aliases: []string{"w"}},
		},
	}

	var code int
	app.Action = func(c *cli.Context) error {
		code = common.RunService(c, cliOpts, false)
		return nil
	}

	require.NoError(t, app.Run([]string{"bento", "-c", badConfigPath}))

	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "Configuration file read error")
}

// TestRunServiceTelemetryResourceErrWriter proves that a fork-only failure
// raised before the service logger exists (here: two telemetry_resources
// entries sharing a label, rejected by telemetry.NewRegistry) is also
// written to CLIOpts.ErrWriter rather than being hard-wired to os.Stderr.
//
// RunService is exercised directly for the same reason as
// TestRunServiceErrWriter: the App's own Action wraps RunService's
// non-zero return code in an os.Exit call.
func TestRunServiceTelemetryResourceErrWriter(t *testing.T) {
	cliOpts := common.NewCLIOpts("1.2.3", "aaa")

	var errBuf bytes.Buffer
	cliOpts.ErrWriter = &errBuf

	confPath := filepath.Join(t.TempDir(), "conf.yaml")
	conf := `
input:
  generate:
    mapping: 'root = {}'
output:
  drop: {}
telemetry_resources:
  - label: dup
    open_telemetry_collector:
      grpc:
        - address: localhost:4317
  - label: dup
    open_telemetry_collector:
      grpc:
        - address: localhost:4317
`
	require.NoError(t, os.WriteFile(confPath, []byte(conf), 0o644))

	app := &cli.App{
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Aliases: []string{"c"}},
			&cli.StringSliceFlag{Name: "set", Aliases: []string{"s"}},
			&cli.StringSliceFlag{Name: "resources", Aliases: []string{"r"}},
			&cli.BoolFlag{Name: "chilled"},
			&cli.BoolFlag{Name: "watcher", Aliases: []string{"w"}},
		},
	}

	var code int
	app.Action = func(c *cli.Context) error {
		code = common.RunService(c, cliOpts, false)
		return nil
	}

	require.NoError(t, app.Run([]string{"bento", "-c", confPath}))

	assert.Equal(t, 1, code)
	assert.Contains(t, errBuf.String(), "Telemetry resource error")
}
