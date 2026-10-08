package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"go.kenn.io/agentsview/internal/db"
	"go.kenn.io/agentsview/internal/fleet"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fleetStoreSpy knows one ingested session.
type fleetStoreSpy struct{ db.Store }

func (fleetStoreSpy) GetSession(_ context.Context, id string) (*db.Session, error) {
	if id != "s-1" {
		return nil, nil
	}
	name := "First run"
	return &db.Session{ID: id, Project: "repo", Machine: "fleet", DisplayName: &name}, nil
}

type fakeLedger struct {
	stories []fleet.Story
	err     error
	// query, when set, receives the query Stories was called with.
	query *fleet.StoryQuery
}

func (f fakeLedger) Stories(_ context.Context, q fleet.StoryQuery) ([]fleet.Story, int, error) {
	if f.query != nil {
		*f.query = q
	}
	return f.stories, len(f.stories), f.err
}

func (f fakeLedger) Story(_ context.Context, id string) (*fleet.Story, error) {
	if f.err != nil {
		return nil, f.err
	}
	for _, s := range f.stories {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, fleet.ErrNotFound
}

func fleetServer(t *testing.T, l fleet.Ledger) *Server {
	t.Helper()
	s := newRoutedTestServerWithStore(t, fleetStoreSpy{})
	s.cfg.Fleet.TraceURL = "https://traces.example.test/{trace_id}"
	if l != nil {
		s.fleet = l
	}
	return s
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(body, &v))
	return v
}

func TestFleetStoriesDisabled(t *testing.T) {
	t.Setenv("AGENTSVIEW_FLEET_LEDGER_DSN", "")
	s := fleetServer(t, nil)

	w := serveGet(t, s, "/api/v1/fleet/stories")
	assertRecorderStatus(t, w, http.StatusOK)
	resp := decode[fleetStoriesResponse](t, w.Body.Bytes())
	assert.False(t, resp.Enabled)
	assert.Empty(t, resp.Stories)

	w = serveGet(t, s, "/api/v1/fleet/stories/x-1")
	assertRecorderStatus(t, w, http.StatusNotFound)
	assert.Contains(t, w.Body.String(), "fleet_disabled")
}

func TestFleetStoriesJoinSessions(t *testing.T) {
	ledger := fakeLedger{stories: []fleet.Story{
		{Issue: fleet.Issue{ID: "x-1", Title: "One"}, State: "pr_open", SessionIDs: []string{"s-1", "s-missing"}},
		{Issue: fleet.Issue{ID: "x-2", Title: "Two"}, State: "running", SessionIDs: []string{}},
	}}
	s := fleetServer(t, ledger)

	w := serveGet(t, s, "/api/v1/fleet/stories?limit=10")
	assertRecorderStatus(t, w, http.StatusOK)
	resp := decode[fleetStoriesResponse](t, w.Body.Bytes())
	require.True(t, resp.Enabled)
	require.Len(t, resp.Stories, 2)
	assert.Equal(t, "https://traces.example.test/{trace_id}", resp.TraceURLTemplate)
	sessions := resp.Stories[0].Sessions
	require.Len(t, sessions, 2)
	assert.Equal(t, fleetSession{ID: "s-1", Exists: true, DisplayName: "First run", Project: "repo", Machine: "fleet"}, sessions[0])
	assert.Equal(t, fleetSession{ID: "s-missing"}, sessions[1])
	assert.Empty(t, resp.Stories[1].Sessions)

	w = serveGet(t, s, "/api/v1/fleet/stories/x-1")
	assertRecorderStatus(t, w, http.StatusOK)
	one := decode[fleetStoryResponse](t, w.Body.Bytes())
	assert.Equal(t, "One", one.Story.Title)
	assert.True(t, one.Story.Sessions[0].Exists)

	w = serveGet(t, s, "/api/v1/fleet/stories/nope")
	assertRecorderStatus(t, w, http.StatusNotFound)
}

func TestFleetStoriesPassesQuery(t *testing.T) {
	var got fleet.StoryQuery
	s := fleetServer(t, fakeLedger{stories: []fleet.Story{{Issue: fleet.Issue{ID: "x-1"}}}, query: &got})

	w := serveGet(t, s, "/api/v1/fleet/stories?q=bands&offset=50&limit=25")
	assertRecorderStatus(t, w, http.StatusOK)
	assert.Equal(t, fleet.StoryQuery{Search: "bands", Offset: 50, Limit: 25}, got)
	assert.Equal(t, 1, decode[fleetStoriesResponse](t, w.Body.Bytes()).Total)

	assertRecorderStatus(t, serveGet(t, s, "/api/v1/fleet/stories?offset=-1"), http.StatusBadRequest)
}

func TestFleetStoriesLedgerError(t *testing.T) {
	s := fleetServer(t, fakeLedger{err: errors.New("ledger down")})

	assertRecorderStatus(t, serveGet(t, s, "/api/v1/fleet/stories"), http.StatusInternalServerError)
	assertRecorderStatus(t, serveGet(t, s, "/api/v1/fleet/stories/x-1"), http.StatusInternalServerError)
}
