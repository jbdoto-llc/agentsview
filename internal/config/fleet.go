package config

import (
	"os"
	"strings"
)

// DefaultFleetDSNEnv is the environment variable read for the fleet ledger
// DSN when [fleet].dsn_env is not set.
const DefaultFleetDSNEnv = "AGENTSVIEW_FLEET_LEDGER_DSN"

// FleetConfig connects the Stories pages to an agent fleet's beads ledgers.
// The DSN carries credentials, so config names the environment variable that
// holds it instead of storing it.
type FleetConfig struct {
	// DSNEnv names the environment variable holding the ledger server's
	// MySQL-protocol DSN, for example user:password@tcp(host:3306)/.
	DSNEnv string `json:"dsn_env,omitempty" toml:"dsn_env"`
	// Databases lists the ledger databases to read, one per ledger.
	Databases []string `json:"databases,omitempty" toml:"databases"`
	// TraceURL is an optional link template for a run's trace; the text
	// {trace_id} is replaced with the run's trace id.
	TraceURL string `json:"trace_url,omitempty" toml:"trace_url"`
}

// DSN returns the ledger DSN from the configured environment variable.
func (c FleetConfig) DSN() string {
	name := strings.TrimSpace(c.DSNEnv)
	if name == "" {
		name = DefaultFleetDSNEnv
	}
	return strings.TrimSpace(os.Getenv(name))
}

// Enabled reports whether the fleet pages have a ledger to read.
func (c FleetConfig) Enabled() bool {
	return len(c.Databases) > 0 && c.DSN() != ""
}
