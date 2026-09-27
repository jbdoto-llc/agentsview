package fleet

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// OpenMySQL opens a pool to a MySQL-protocol ledger server (such as a Dolt
// sql-server) from a go-sql-driver DSN. Time parsing and bounded timeouts
// are forced so a slow or unreachable ledger fails fast instead of hanging
// the API.
func OpenMySQL(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("fleet: parse ledger DSN: %w", err)
	}
	cfg.ParseTime = true
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 15 * time.Second
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("fleet: open ledger: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}
