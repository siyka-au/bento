package telemetry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistryEmptyLabelError(t *testing.T) {
	_, err := NewRegistry([]Config{baseConf("")}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "label")
}

func TestNewRegistryDuplicateLabelError(t *testing.T) {
	_, err := NewRegistry([]Config{baseConf("dupe"), baseConf("dupe")}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dupe")
}

func TestNewRegistryGet(t *testing.T) {
	reg, err := NewRegistry([]Config{baseConf("a"), baseConf("b")}, "1.0.0")
	require.NoError(t, err)

	res, err := reg.Get("a")
	require.NoError(t, err)
	assert.Equal(t, "a", res.Label())

	_, err = reg.Get("unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "a")
	assert.Contains(t, err.Error(), "b")
}

func TestRegistryGetNilReceiverError(t *testing.T) {
	var reg *Registry
	_, err := reg.Get("anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anything")
}
