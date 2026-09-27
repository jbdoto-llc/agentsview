package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFleetConfig(t *testing.T) {
	tests := []struct {
		name      string
		toml      string
		env       map[string]string
		databases []string
		dsn       string
		enabled   bool
	}{
		{name: "absent section is disabled"},
		{
			name:      "databases with the default DSN variable",
			toml:      "[fleet]\ndatabases = [\"alpha\", \"beta\"]\ntrace_url = \" https://traces.example.test/{trace_id} \"\n",
			env:       map[string]string{DefaultFleetDSNEnv: "u:p@tcp(ledger.example.test:3306)/"},
			databases: []string{"alpha", "beta"},
			dsn:       "u:p@tcp(ledger.example.test:3306)/",
			enabled:   true,
		},
		{
			name:      "a custom DSN variable",
			toml:      "[fleet]\ndsn_env = \"MY_LEDGER\"\ndatabases = [\"alpha\"]\n",
			env:       map[string]string{"MY_LEDGER": "u:p@tcp(h:3306)/"},
			databases: []string{"alpha"},
			dsn:       "u:p@tcp(h:3306)/",
			enabled:   true,
		},
		{
			name:      "databases without a DSN stay disabled",
			toml:      "[fleet]\ndatabases = [\"alpha\"]\n",
			databases: []string{"alpha"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(DefaultFleetDSNEnv, "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg, err := Default()
			require.NoError(t, err)
			require.NoError(t, cfg.applyConfigTOML(tt.toml))
			assert.Equal(t, tt.databases, cfg.Fleet.Databases)
			assert.Equal(t, tt.dsn, cfg.Fleet.DSN())
			assert.Equal(t, tt.enabled, cfg.Fleet.Enabled())
			if tt.name == "databases with the default DSN variable" {
				assert.Equal(t, "https://traces.example.test/{trace_id}", cfg.Fleet.TraceURL)
			}
		})
	}
}
