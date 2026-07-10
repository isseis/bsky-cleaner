package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveHostname_TOMLValueSet_ReturnsTOMLValue(t *testing.T) {
	cfg := Config{Hostname: "worker-1"}
	got, err := ResolveHostname(cfg)
	require.NoError(t, err)
	assert.Equal(t, "worker-1", got)
}

func TestResolveHostname_TOMLValueEmpty_ReturnsOSHostname(t *testing.T) {
	hostname, err := os.Hostname()
	if err != nil {
		t.Skipf("os.Hostname() failed: %v", err)
	}
	cfg := Config{Hostname: ""}
	got, err := ResolveHostname(cfg)
	require.NoError(t, err)
	assert.Equal(t, hostname, got)
}

func TestLoad_HostnameField_ParsesOptionalTOMLKey(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "key present",
			content: `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
hostname = "worker-1"
`,
			want: "worker-1",
		},
		{
			name: "key absent",
			content: `
retention_days = 30
schedule = "0 3 * * *"
execution_timeout_seconds = 3600
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempTOML(t, tt.content)

			cfg, err := Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Hostname)
		})
	}
}
