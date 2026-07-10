package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveHostname_TOMLValueSet_ReturnsTOMLValue(t *testing.T) {
	cfg := Config{Hostname: "worker-1"}
	got := ResolveHostname(cfg)
	assert.Equal(t, "worker-1", got)
}

func TestResolveHostname_TOMLValueEmpty_ReturnsOSHostname(t *testing.T) {
	cfg := Config{Hostname: ""}
	got := ResolveHostname(cfg)
	assert.NotEmpty(t, got)
}
