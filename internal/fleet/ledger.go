package fleet

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrNotFound reports that a story id is in none of the configured ledgers.
var ErrNotFound = errors.New("story not found")

// Ledger reads fleet stories.
type Ledger interface {
	// Stories returns the issues that have fleet events, most recent
	// activity first, without their event lists.
	Stories(ctx context.Context, limit int) ([]Story, error)
	// Story returns one story with its events.
	Story(ctx context.Context, id string) (*Story, error)
}

var identRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// SQLLedger reads beads ledgers (issues, labels, comments tables) from a SQL
// server. Each entry in Databases is one ledger; its tables are addressed as
// `db`.table so one connection serves every ledger.
type SQLLedger struct {
	db        *sql.DB
	databases []string
}

// NewSQLLedger wraps an open connection. Database names must be plain
// identifiers; they are interpolated into queries.
func NewSQLLedger(db *sql.DB, databases []string) (*SQLLedger, error) {
	if len(databases) == 0 {
		return nil, errors.New("fleet: no ledger databases configured")
	}
	for _, d := range databases {
		if !identRe.MatchString(d) {
			return nil, fmt.Errorf("fleet: invalid ledger database name %q", d)
		}
	}
	return &SQLLedger{db: db, databases: databases}, nil
}

// Stories implements Ledger.
func (l *SQLLedger) Stories(ctx context.Context, limit int) ([]Story, error) {
	var out []Story
	for _, d := range l.databases {
		events, err := l.events(ctx, d, "")
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(events))
		for id := range events {
			ids = append(ids, id)
		}
		issues, err := l.issues(ctx, d, ids)
		if err != nil {
			return nil, err
		}
		for _, is := range issues {
			s := BuildStory(is, events[is.ID])
			s.Events = nil
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastEvent.After(out[j].LastEvent) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []Story{}
	}
	return out, nil
}

// Story implements Ledger.
func (l *SQLLedger) Story(ctx context.Context, id string) (*Story, error) {
	for _, d := range l.databases {
		issues, err := l.issues(ctx, d, []string{id})
		if err != nil {
			return nil, err
		}
		if len(issues) == 0 {
			continue
		}
		events, err := l.events(ctx, d, id)
		if err != nil {
			return nil, err
		}
		s := BuildStory(issues[0], events[id])
		return &s, nil
	}
	return nil, ErrNotFound
}

// events returns fleet events by issue id, for one issue or all of them.
func (l *SQLLedger) events(ctx context.Context, d, id string) (map[string][]Event, error) {
	q := fmt.Sprintf("SELECT issue_id, text, created_at FROM `%s`.comments WHERE text LIKE ?", d)
	args := []any{EventPrefix + "%"}
	if id != "" {
		q += " AND issue_id = ?"
		args = append(args, id)
	}
	rows, err := l.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("fleet: read %s comments: %w", d, err)
	}
	defer rows.Close()
	out := map[string][]Event{}
	for rows.Next() {
		var c Comment
		var created sql.NullTime
		if err := rows.Scan(&c.IssueID, &c.Text, &created); err != nil {
			return nil, fmt.Errorf("fleet: scan %s comment: %w", d, err)
		}
		c.CreatedAt = created.Time
		if ev, ok := ParseEvent(c); ok {
			out[c.IssueID] = append(out[c.IssueID], ev)
		}
	}
	return out, rows.Err()
}

// issues returns the issue rows (with labels) for ids in one ledger.
func (l *SQLLedger) issues(ctx context.Context, d string, ids []string) ([]Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := l.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT id, title, status, priority, COALESCE(assignee, ''), created_at, updated_at, closed_at FROM `%s`.issues WHERE id IN (%s)", d, ph), args...)
	if err != nil {
		return nil, fmt.Errorf("fleet: read %s issues: %w", d, err)
	}
	defer rows.Close()
	var out []Issue
	byID := map[string]int{}
	for rows.Next() {
		var is Issue
		var created, updated, closed sql.NullTime
		if err := rows.Scan(&is.ID, &is.Title, &is.Status, &is.Priority, &is.Assignee, &created, &updated, &closed); err != nil {
			return nil, fmt.Errorf("fleet: scan %s issue: %w", d, err)
		}
		is.Ledger, is.CreatedAt, is.UpdatedAt, is.Labels = d, created.Time, updated.Time, []string{}
		if closed.Valid {
			t := closed.Time
			is.ClosedAt = &t
		}
		byID[is.ID] = len(out)
		out = append(out, is)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	lrows, err := l.db.QueryContext(ctx, fmt.Sprintf(
		"SELECT issue_id, label FROM `%s`.labels WHERE issue_id IN (%s) ORDER BY label", d, ph), args...)
	if err != nil {
		return nil, fmt.Errorf("fleet: read %s labels: %w", d, err)
	}
	defer lrows.Close()
	for lrows.Next() {
		var id, label string
		if err := lrows.Scan(&id, &label); err != nil {
			return nil, fmt.Errorf("fleet: scan %s label: %w", d, err)
		}
		if i, ok := byID[id]; ok {
			out[i].Labels = append(out[i].Labels, label)
		}
	}
	return out, lrows.Err()
}

// Ping checks the connection within a short deadline.
func (l *SQLLedger) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return l.db.PingContext(ctx)
}
