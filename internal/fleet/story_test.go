package fleet

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		ok     bool
		want   Event
		detail map[string]any
	}{
		{
			name:   "current format uses its own timestamp",
			text:   `fleet-event {"t":"2026-01-02T03:05:00Z","role":"worker","event":"worker_started","run":"w-1","branch":"b"}`,
			ok:     true,
			want:   Event{At: at(1), Role: "worker", Name: "worker_started", Run: "w-1"},
			detail: map[string]any{"branch": "b"},
		},
		{
			name:   "legacy record falls back to the comment time",
			text:   `fleet-event {"event":"pr_opened","url":"https://example.test/pr/1","cost_usd":0.5}`,
			ok:     true,
			want:   Event{At: at(0), Name: "pr_opened"},
			detail: map[string]any{"url": "https://example.test/pr/1", "cost_usd": 0.5},
		},
		{name: "ordinary comment", text: "PR opened: https://example.test/pr/1"},
		{name: "malformed payload", text: "fleet-event {not json"},
		{name: "payload without event name", text: `fleet-event {"t":"2026-01-02T03:05:00Z"}`},
		{name: "payload is not an object", text: `fleet-event ["claimed"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseEvent(Comment{IssueID: "x-1", Text: tt.text, CreatedAt: at(0)})
			require.Equal(t, tt.ok, ok)
			if !tt.ok {
				return
			}
			assert.Equal(t, tt.want.At, got.At)
			assert.Equal(t, tt.want.Role, got.Role)
			assert.Equal(t, tt.want.Name, got.Name)
			assert.Equal(t, tt.want.Run, got.Run)
			assert.Equal(t, tt.detail, got.Detail)
		})
	}
}

func ev(min int, name, run string, detail map[string]any) Event {
	return Event{At: at(min), Name: name, Run: run, Detail: detail}
}

func finished(status string, extra map[string]any) map[string]any {
	d := map[string]any{"status": status}
	for k, v := range extra {
		d[k] = v
	}
	return d
}

func TestBuildStory(t *testing.T) {
	open := Issue{ID: "x-1", Title: "Do the thing", Status: "in_progress"}
	ok := finished("ok", map[string]any{
		"duration_s": 24.0, "claude_ms": 10704.0, "turns": 3.0, "cost_usd": 0.1,
		"model": "model-a", "session": "s-2", "trace_id": "tr-2", "transcript": "s3://b/k.jsonl",
		"pr": 552.0, "pr_url": "https://example.test/pr/552",
		"usage": map[string]any{"input": 6.0, "output": 343.0, "cache_read": 43470.0, "cache_creation": 6901.0},
	})

	tests := []struct {
		name     string
		issue    Issue
		events   []Event
		state    string
		attempts int
		cost     float64
		statuses []string
		check    func(t *testing.T, s Story)
	}{
		{
			name:  "success opens a PR and records usage and links",
			issue: open,
			events: []Event{
				ev(0, "claimed", "w-2", map[string]any{"due": "later"}),
				ev(1, "worker_started", "w-2", map[string]any{"branch": "b", "model": "model-a", "trace_id": "tr-2"}),
				ev(2, "worker_finished", "w-2", ok),
			},
			state: "pr_open", attempts: 1, cost: 0.1, statuses: []string{"ok"},
			check: func(t *testing.T, s Story) {
				r := s.Runs[0]
				assert.Equal(t, "s-2", r.SessionID)
				assert.Equal(t, "tr-2", r.TraceID)
				assert.Equal(t, 552, r.PRNumber)
				assert.Equal(t, 3, r.Turns)
				assert.Equal(t, Tokens{Input: 6, Output: 343, CacheRead: 43470, CacheCreation: 6901}, r.Tokens)
				assert.Equal(t, "s3://b/k.jsonl", r.Transcript)
				assert.Equal(t, []string{"s-2"}, s.SessionIDs)
				assert.Equal(t, "https://example.test/pr/552", s.PRURL)
				require.NotNil(t, r.ClaimedAt)
				assert.Equal(t, at(0), *r.ClaimedAt)
				assert.Equal(t, at(2), s.LastEvent)
			},
		},
		{
			name:  "a failed attempt then a successful retry",
			issue: open,
			events: []Event{
				ev(0, "claimed", "w-1", nil),
				ev(1, "worker_started", "w-1", nil),
				ev(2, "worker_finished", "w-1", finished("failed", map[string]any{"reason": "claude exited 1", "cost_usd": 0.02})),
				ev(3, "released", "w-1", map[string]any{"reason": "claude exited 1"}),
				ev(10, "claimed", "w-2", nil),
				ev(11, "worker_started", "w-2", nil),
				ev(12, "worker_finished", "w-2", ok),
			},
			state: "pr_open", attempts: 2, cost: 0.12, statuses: []string{"failed", "ok"},
			check: func(t *testing.T, s Story) {
				assert.Equal(t, "claude exited 1", s.Runs[0].Reason)
				assert.Equal(t, "w-2", s.LatestRun.ID)
			},
		},
		{
			name:  "legacy records without run ids",
			issue: open,
			events: []Event{
				ev(0, "started", "", map[string]any{"branch": "b"}),
				ev(1, "pr_opened", "", map[string]any{"url": "https://example.test/pr/548", "cost_usd": 0.11}),
			},
			state: "pr_open", attempts: 1, cost: 0.11, statuses: []string{"ok"},
		},
		{
			name:  "a sandbox lost after the claim is reclaimed as failed",
			issue: Issue{ID: "x-1", Status: "open"},
			events: []Event{
				ev(0, "claimed", "w-1", nil),
				ev(1, "worker_started", "w-1", nil),
				ev(9, "reclaimed", "", map[string]any{"reason": "sandbox_lost"}),
			},
			state: "failed", attempts: 1, statuses: []string{"failed"},
			check: func(t *testing.T, s Story) { assert.Equal(t, "sandbox_lost", s.Runs[0].Reason) },
		},
		{
			name:   "a run in progress",
			issue:  open,
			events: []Event{ev(0, "claimed", "w-1", nil), ev(1, "worker_started", "w-1", nil)},
			state:  "running", attempts: 1, statuses: []string{"running"},
		},
		{
			name:  "no runs falls back to the issue status",
			issue: Issue{ID: "x-1", Status: "open"},
			state: "open", statuses: []string{},
		},
		{
			name:   "a closed issue is closed whatever its runs say",
			issue:  Issue{ID: "x-1", Status: "closed"},
			events: []Event{ev(0, "claimed", "w-1", nil)},
			state:  "closed", statuses: []string{"claimed"},
		},
		{
			name:  "events arriving out of order are sorted first",
			issue: open,
			events: []Event{
				ev(2, "worker_finished", "w-2", ok),
				ev(0, "claimed", "w-2", nil),
				ev(1, "worker_started", "w-2", nil),
			},
			state: "pr_open", attempts: 1, cost: 0.1, statuses: []string{"ok"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := BuildStory(tt.issue, tt.events)
			assert.Equal(t, tt.state, s.State)
			assert.Equal(t, tt.attempts, s.Attempts)
			assert.InDelta(t, tt.cost, s.CostUSD, 1e-9)
			statuses := []string{}
			for _, r := range s.Runs {
				statuses = append(statuses, r.Status)
			}
			assert.Equal(t, tt.statuses, statuses)
			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}
}
