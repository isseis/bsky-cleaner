package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
