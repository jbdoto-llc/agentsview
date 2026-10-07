// Package fleet reads an agent fleet's work ledger (beads issues whose
// comments carry `fleet-event` records) and folds each issue's events into a
// story: the runs that worked on it, their outcome, cost, and the links that
// join a story to its sessions, traces, and pull requests.
package fleet

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// EventPrefix marks a ledger comment as a machine-readable fleet event.
const EventPrefix = "fleet-event "

// Event is one fleet-event record from a ledger comment.
type Event struct {
	// At is when the event happened: the record's own "t" field when
	// present, otherwise the comment's creation time.
	At     time.Time      `json:"at"`
	Role   string         `json:"role,omitempty"`
	Name   string         `json:"event"`
	Run    string         `json:"run,omitempty"`
	Detail map[string]any `json:"detail,omitempty"`
}

// Comment is a ledger comment as stored.
type Comment struct {
	IssueID   string
	Text      string
	CreatedAt time.Time
}

// ParseEvent decodes a comment into an Event. It reports false for comments
// that are not fleet events or whose payload is not a JSON object.
func ParseEvent(c Comment) (Event, bool) {
	if !strings.HasPrefix(c.Text, EventPrefix) {
		return Event{}, false
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(c.Text, EventPrefix)), &raw); err != nil {
		return Event{}, false
	}
	name, _ := raw["event"].(string)
	if name == "" {
		return Event{}, false
	}
	ev := Event{At: c.CreatedAt, Name: name}
	if t, ok := raw["t"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			ev.At = parsed
		}
	}
	ev.Role, _ = raw["role"].(string)
	ev.Run, _ = raw["run"].(string)
	for _, k := range []string{"t", "role", "event", "run"} {
		delete(raw, k)
	}
	if len(raw) > 0 {
		ev.Detail = raw
	}
	return ev, true
}

// Issue is the ledger row a story is built around.
type Issue struct {
	Ledger    string     `json:"ledger"`
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Priority  int        `json:"priority"`
	Assignee  string     `json:"assignee,omitempty"`
	Labels    []string   `json:"labels"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
}

// Tokens is a run's token usage by kind.
type Tokens struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreation int64 `json:"cache_creation"`
}

// Run is one attempt at a story, keyed by the ledger's run identifier.
type Run struct {
	ID string `json:"id"`
	// Role is the fleet role that ran the attempt, from its start record:
	// "worker" writes the change, "review" reviews its pull request.
	Role       string     `json:"role,omitempty"`
	Status     string     `json:"status"` // claimed, running, ok, failed
	ClaimedAt  *time.Time `json:"claimed_at,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	DurationS  float64    `json:"duration_s,omitempty"`
	AgentMs    float64    `json:"agent_ms,omitempty"`
	Turns      int        `json:"turns,omitempty"`
	CostUSD    float64    `json:"cost_usd,omitempty"`
	Tokens     Tokens     `json:"tokens"`
	Model      string     `json:"model,omitempty"`
	Branch     string     `json:"branch,omitempty"`
	SessionID  string     `json:"session_id,omitempty"`
	TraceID    string     `json:"trace_id,omitempty"`
	Transcript string     `json:"transcript,omitempty"`
	PRNumber   int        `json:"pr_number,omitempty"`
	PRURL      string     `json:"pr_url,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

// Story is an issue plus everything the fleet recorded while working on it.
type Story struct {
	Issue
	// State summarizes where the story stands: running, pr_open, failed,
	// closed, or the issue status when no run has been recorded.
	State      string    `json:"state"`
	Attempts   int       `json:"attempts"`
	CostUSD    float64   `json:"cost_usd"`
	LastEvent  time.Time `json:"last_event"`
	PRURL      string    `json:"pr_url,omitempty"`
	LatestRun  *Run      `json:"latest_run,omitempty"`
	Runs       []Run     `json:"runs"`
	Events     []Event   `json:"events,omitempty"`
	SessionIDs []string  `json:"session_ids"`
}

// BuildStory folds an issue's fleet events, in any order, into a Story.
// Events are always included; callers drop them for list views.
func BuildStory(issue Issue, events []Event) Story {
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	s := Story{Issue: issue, Events: events, Runs: []Run{}, SessionIDs: []string{}}
	idx := map[string]int{}
	current := -1
	byID := func(id string) *Run {
		if i, ok := idx[id]; ok && id != "" {
			current = i
			return &s.Runs[i]
		}
		return nil
	}
	create := func(id string) *Run {
		s.Runs = append(s.Runs, Run{ID: id, Status: "claimed"})
		current = len(s.Runs) - 1
		if id != "" {
			idx[id] = current
		}
		return &s.Runs[current]
	}
	// latest is where records without a run id land (older records carried
	// none, and a dispatcher reclaim has no run to name).
	latest := func() *Run {
		if current >= 0 {
			return &s.Runs[current]
		}
		return nil
	}
	existing := func(ev Event) *Run {
		if r := byID(ev.Run); r != nil {
			return r
		}
		if ev.Run == "" {
			if r := latest(); r != nil {
				return r
			}
		}
		return create(ev.Run)
	}
	for _, ev := range events {
		at := ev.At
		if at.After(s.LastEvent) {
			s.LastEvent = at
		}
		switch ev.Name {
		case "claimed":
			r := byID(ev.Run)
			if r == nil {
				r = create(ev.Run)
			}
			r.ClaimedAt = &at
		case "worker_started", "started":
			r := byID(ev.Run)
			if r == nil && ev.Run == "" {
				// A legacy start reuses a claimed-but-unstarted run; a second
				// start without a run id is a new attempt.
				if l := latest(); l != nil && l.StartedAt == nil && l.FinishedAt == nil {
					r = l
				}
			}
			if r == nil {
				r = create(ev.Run)
			}
			r.Status = "running"
			r.StartedAt = &at
			if ev.Role != "" {
				r.Role = ev.Role
			}
			r.Branch = str(ev.Detail, "branch", r.Branch)
			r.Model = str(ev.Detail, "model", r.Model)
			r.TraceID = str(ev.Detail, "trace_id", r.TraceID)
		case "worker_finished":
			finish(existing(ev), ev, at)
		case "pr_opened": // legacy success record
			r := existing(ev)
			r.Status = "ok"
			r.FinishedAt = &at
			r.PRURL = str(ev.Detail, "url", r.PRURL)
			r.CostUSD = num(ev.Detail, "cost_usd", r.CostUSD)
		case "failed", "released", "reclaimed":
			r := existing(ev)
			if r.Status != "ok" {
				r.Status = "failed"
				if r.FinishedAt == nil {
					r.FinishedAt = &at
				}
			}
			r.Reason = str(ev.Detail, "reason", r.Reason)
		}
	}
	for i := range s.Runs {
		r := s.Runs[i]
		s.CostUSD += r.CostUSD
		if r.StartedAt != nil {
			s.Attempts++
		}
		if r.SessionID != "" {
			s.SessionIDs = append(s.SessionIDs, r.SessionID)
		}
		if r.PRURL != "" {
			s.PRURL = r.PRURL
		}
	}
	if n := len(s.Runs); n > 0 {
		latest := s.Runs[n-1]
		s.LatestRun = &latest
	}
	s.State = storyState(s)
	return s
}

func finish(r *Run, ev Event, at time.Time) {
	d := ev.Detail
	r.FinishedAt = &at
	if st := str(d, "status", ""); st != "" {
		r.Status = st
	}
	r.DurationS = num(d, "duration_s", r.DurationS)
	r.AgentMs = num(d, "claude_ms", r.AgentMs)
	r.Turns = int(num(d, "turns", float64(r.Turns)))
	r.CostUSD = num(d, "cost_usd", r.CostUSD)
	r.Model = str(d, "model", r.Model)
	r.SessionID = str(d, "session", r.SessionID)
	r.TraceID = str(d, "trace_id", r.TraceID)
	r.Transcript = str(d, "transcript", r.Transcript)
	r.PRURL = str(d, "pr_url", r.PRURL)
	r.PRNumber = int(num(d, "pr", float64(r.PRNumber)))
	r.Reason = str(d, "reason", r.Reason)
	if u, ok := d["usage"].(map[string]any); ok {
		r.Tokens = Tokens{
			Input:         int64(num(u, "input", 0)),
			Output:        int64(num(u, "output", 0)),
			CacheRead:     int64(num(u, "cache_read", 0)),
			CacheCreation: int64(num(u, "cache_creation", 0)),
		}
	}
}

func storyState(s Story) string {
	if s.Status == "closed" {
		return "closed"
	}
	if s.LatestRun == nil {
		return s.Status
	}
	switch s.LatestRun.Status {
	case "claimed", "running":
		return "running"
	case "ok":
		if s.LatestRun.PRURL != "" {
			return "pr_open"
		}
		return "done"
	default:
		return "failed"
	}
}

func str(d map[string]any, k, fallback string) string {
	if v, ok := d[k].(string); ok && v != "" {
		return v
	}
	return fallback
}

func num(d map[string]any, k string, fallback float64) float64 {
	if v, ok := d[k].(float64); ok {
		return v
	}
	return fallback
}
