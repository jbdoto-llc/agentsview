package fleet

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// ledgerDB returns an in-memory database with two attached beads ledgers
// ("alpha", "beta") holding the tables SQLLedger reads.
func ledgerDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, d := range []string{"alpha", "beta"} {
		_, err := db.Exec("ATTACH DATABASE ':memory:' AS " + d)
		require.NoError(t, err)
		for _, stmt := range []string{
			"CREATE TABLE " + d + ".issues (id TEXT PRIMARY KEY, title TEXT, status TEXT, priority INTEGER, assignee TEXT, created_at DATETIME, updated_at DATETIME, closed_at DATETIME)",
			"CREATE TABLE " + d + ".labels (issue_id TEXT, label TEXT)",
			"CREATE TABLE " + d + ".comments (id TEXT, issue_id TEXT, author TEXT, text TEXT, created_at DATETIME)",
		} {
			_, err := db.Exec(stmt)
			require.NoError(t, err)
		}
	}
	exec := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err)
	}
	exec("INSERT INTO alpha.issues VALUES ('a-1','First','in_progress',2,'bot',?,?,NULL)", at(0), at(5))
	exec("INSERT INTO alpha.issues VALUES ('a-2','No fleet events','open',2,NULL,?,?,NULL)", at(0), at(0))
	exec("INSERT INTO beta.issues VALUES ('b-1','Second','closed',1,NULL,?,?,?)", at(0), at(30), at(30))
	exec("INSERT INTO alpha.labels VALUES ('a-1','spike'),('a-1','has-pr')")
	for _, c := range []struct {
		db, issue, text string
		min             int
	}{
		{"alpha", "a-1", `fleet-event {"t":"2026-01-02T03:04:00Z","role":"dispatch","event":"claimed","run":"w-1"}`, 0},
		{"alpha", "a-1", `fleet-event {"t":"2026-01-02T03:05:00Z","role":"worker","event":"worker_started","run":"w-1"}`, 1},
		{"alpha", "a-1", `fleet-event {"t":"2026-01-02T03:06:00Z","role":"worker","event":"worker_finished","run":"w-1","status":"ok","cost_usd":0.1,"session":"s-1","pr_url":"https://example.test/pr/1"}`, 2},
		{"alpha", "a-1", "PR opened: https://example.test/pr/1", 2},
		{"alpha", "a-2", "just a human comment", 1},
		{"beta", "b-1", `fleet-event {"t":"2026-01-02T03:20:00Z","role":"worker","event":"worker_started","run":"w-9"}`, 16},
		{"beta", "b-1", `fleet-event {"t":"2026-01-02T03:25:00Z","role":"worker","event":"worker_finished","run":"w-9","status":"failed","reason":"boom"}`, 21},
	} {
		exec("INSERT INTO "+c.db+".comments VALUES ('c',?, 'bot', ?, ?)", c.issue, c.text, at(c.min))
	}
	return db
}

func TestSQLLedgerStories(t *testing.T) {
	l, err := NewSQLLedger(ledgerDB(t), []string{"alpha", "beta"})
	require.NoError(t, err)

	stories, err := l.Stories(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, stories, 2, "only issues with fleet events are stories")
	assert.Equal(t, "b-1", stories[0].ID, "most recent activity first")
	assert.Equal(t, "beta", stories[0].Ledger)
	assert.Equal(t, "closed", stories[0].State)
	assert.Equal(t, "a-1", stories[1].ID)
	assert.Equal(t, "pr_open", stories[1].State)
	assert.Equal(t, []string{"has-pr", "spike"}, stories[1].Labels)
	assert.Equal(t, []string{"s-1"}, stories[1].SessionIDs)
	assert.Nil(t, stories[1].Events, "list view omits events")

	limited, err := l.Stories(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, limited, 1)
}

func TestSQLLedgerStory(t *testing.T) {
	l, err := NewSQLLedger(ledgerDB(t), []string{"alpha", "beta"})
	require.NoError(t, err)

	s, err := l.Story(context.Background(), "b-1")
	require.NoError(t, err)
	assert.Equal(t, "beta", s.Ledger)
	require.Len(t, s.Events, 2)
	require.Len(t, s.Runs, 1)
	assert.Equal(t, "boom", s.Runs[0].Reason)
	require.NotNil(t, s.ClosedAt)

	_, err = l.Story(context.Background(), "nope")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestNewSQLLedgerValidatesDatabaseNames(t *testing.T) {
	tests := []struct {
		name string
		dbs  []string
		ok   bool
	}{
		{"plain names", []string{"alpha", "beta_2"}, true},
		{"none", nil, false},
		{"injection attempt", []string{"alpha`; DROP TABLE x; --"}, false},
		{"dotted", []string{"a.b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSQLLedger(&sql.DB{}, tt.dbs)
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestOpenMySQLRejectsBadDSN(t *testing.T) {
	_, err := OpenMySQL("not a dsn")
	assert.Error(t, err)
	db, err := OpenMySQL("user:secret@tcp(ledger.example.test:3306)/")
	require.NoError(t, err, "opening does not dial")
	_ = db.Close()
}
