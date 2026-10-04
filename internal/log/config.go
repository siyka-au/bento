package log

import "github.com/warpstreamlabs/bento/internal/docs"

const (
	fieldLogLevel         = "level"
	fieldFormat           = "format"
	fieldAddTimeStamp     = "add_timestamp"
	fieldLevelName        = "level_name"
	fieldMessageName      = "message_name"
	fieldTimestampName    = "timestamp_name"
	fieldStaticFields     = "static_fields"
	fieldFile             = "file"
	fieldFilePath         = "path"
	fieldFileRotate       = "rotate"
	fieldFileRotateMaxAge = "rotate_max_age_days"
	fieldLogAllErrors     = "log_all_errors"
	fieldOtel             = "otel"
	fieldOtelEnabled      = "enabled"
	fieldOtelGRPC         = "grpc"
	fieldOtelHTTP         = "http"
	fieldOtelTags         = "tags"
	fieldOtelAddress      = "address"
	fieldOtelSecure       = "secure"
)

// Config holds configuration options for a logger object.
//
// There's exactly one logger per process, unlike metrics exporters or tracer
// providers (internal/component/metrics, internal/component/tracer), which
// are legitimately swappable backends selected via a bundle-style `type:`
// field. Forcing logger output through the same registration mechanism would
// buy config-shape consistency at the cost of a parallel LoggersInit bundle
// for a single-instance concern, so OTel export is instead an optional
// sub-block (Otel) fanned out alongside the primary file/stdout handler.
type Config struct {
	LogLevel      string            `yaml:"level"`
	Format        string            `yaml:"format"`
	AddTimeStamp  bool              `yaml:"add_timestamp"`
	LogAllErrors  bool              `yaml:"log_all_errors"`
	LevelName     string            `yaml:"level_name"`
	MessageName   string            `yaml:"message_name"`
	TimestampName string            `yaml:"timestamp_name"`
	StaticFields  map[string]string `yaml:"static_fields"`
	File          File              `yaml:"file"`
	Otel          OtelConfig        `yaml:"otel"`
}

// File contains configuration for file based logging.
type File struct {
	Path         string `yaml:"path"`
	Rotate       bool   `yaml:"rotate"`
	RotateMaxAge int    `yaml:"rotate_max_age_days"`
}

// OtelCollector describes a single OTLP collector endpoint to export logs to.
type OtelCollector struct {
	Address string `yaml:"address"`
	Secure  bool   `yaml:"secure"`
}

// OtelConfig contains configuration for exporting logs to an OpenTelemetry
// collector via OTLP, fanned out alongside the primary file/stdout output.
type OtelConfig struct {
	Enabled bool              `yaml:"enabled"`
	GRPC    []OtelCollector   `yaml:"grpc"`
	HTTP    []OtelCollector   `yaml:"http"`
	Tags    map[string]string `yaml:"tags"`
}

// NewConfig returns a config struct with the default values for each field.
func NewConfig() Config {
	return Config{
		LogLevel:      "INFO",
		Format:        "logfmt",
		AddTimeStamp:  false,
		LogAllErrors:  false,
		LevelName:     "level",
		TimestampName: "time",
		MessageName:   "msg",
		StaticFields: map[string]string{
			"@service": "bento",
		},
	}
}

// UnmarshalYAML ensures that when parsing configs that are in a slice the
// default values are still applied.
func (conf *Config) UnmarshalYAML(unmarshal func(any) error) error {
	type confAlias Config
	aliased := confAlias(NewConfig())

	defaultFields := aliased.StaticFields
	aliased.StaticFields = nil

	if err := unmarshal(&aliased); err != nil {
		return err
	}

	if aliased.StaticFields == nil {
		aliased.StaticFields = defaultFields
	}

	*conf = Config(aliased)
	return nil
}

func FromParsed(pConf *docs.ParsedConfig) (conf Config, err error) {
	if conf.LogLevel, err = pConf.FieldString(fieldLogLevel); err != nil {
		return
	}
	if conf.Format, err = pConf.FieldString(fieldFormat); err != nil {
		return
	}
	if conf.AddTimeStamp, err = pConf.FieldBool(fieldAddTimeStamp); err != nil {
		return
	}
	if conf.LogAllErrors, err = pConf.FieldBool(fieldLogAllErrors); err != nil {
		return
	}
	if conf.LevelName, err = pConf.FieldString(fieldLevelName); err != nil {
		return
	}
	if conf.MessageName, err = pConf.FieldString(fieldMessageName); err != nil {
		return
	}
	if conf.TimestampName, err = pConf.FieldString(fieldTimestampName); err != nil {
		return
	}
	if conf.StaticFields, err = pConf.FieldStringMap(fieldStaticFields); err != nil {
		return
	}

	if pConf.Contains(fieldFile) {
		fConf := pConf.Namespace(fieldFile)
		if conf.File.Path, err = fConf.FieldString(fieldFilePath); err != nil {
			return
		}
		if conf.File.Rotate, err = fConf.FieldBool(fieldFileRotate); err != nil {
			return
		}
		if conf.File.RotateMaxAge, err = fConf.FieldInt(fieldFileRotateMaxAge); err != nil {
			return
		}
	}

	if pConf.Contains(fieldOtel) {
		oConf := pConf.Namespace(fieldOtel)
		if conf.Otel, err = otelConfigFromParsed(oConf); err != nil {
			return
		}
	}
	return
}

func otelConfigFromParsed(oConf *docs.ParsedConfig) (conf OtelConfig, err error) {
	if conf.Enabled, err = oConf.FieldBool(fieldOtelEnabled); err != nil {
		return
	}
	if conf.GRPC, err = otelCollectorsFromParsed(oConf, fieldOtelGRPC); err != nil {
		return
	}
	if conf.HTTP, err = otelCollectorsFromParsed(oConf, fieldOtelHTTP); err != nil {
		return
	}
	if conf.Tags, err = oConf.FieldStringMap(fieldOtelTags); err != nil {
		return
	}
	return
}

func otelCollectorsFromParsed(oConf *docs.ParsedConfig, field string) ([]OtelCollector, error) {
	list, err := oConf.FieldObjectList(field)
	if err != nil {
		return nil, err
	}
	collectors := make([]OtelCollector, 0, len(list))
	for _, c := range list {
		address, err := c.FieldString(fieldOtelAddress)
		if err != nil {
			return nil, err
		}
		secure, err := c.FieldBool(fieldOtelSecure)
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, OtelCollector{Address: address, Secure: secure})
	}
	return collectors, nil
}
