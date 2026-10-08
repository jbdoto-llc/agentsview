package server

import (
	"context"
	"errors"
	"net/http"

	"go.kenn.io/agentsview/internal/fleet"

	"github.com/danielgtaylor/huma/v2"
)

func (s *Server) registerFleetRoutes() {
	group := huma.NewGroup(s.api, "/api/v1")
	configureRouteGroup(group, "Fleet")

	s.get(group, "/fleet/stories", "List fleet stories", s.humaListFleetStories)
	s.get(group, "/fleet/stories/{id}", "Get fleet story", s.humaGetFleetStory)
}

type fleetStoriesInput struct {
	Q      string `query:"q" maxLength:"200" doc:"Keep stories whose id, title, or a label contains this text, ignoring case"`
	Offset int    `query:"offset" minimum:"0" doc:"Matching stories to skip, most recent activity first"`
	Limit  int    `query:"limit" default:"200" minimum:"1" maximum:"1000" doc:"Maximum stories to return, most recent activity first"`
}

type fleetStoryInput struct {
	ID string `path:"id" required:"true" doc:"Story (ledger issue) ID"`
}

// fleetSession is a session a story's runs recorded, joined against this
// archive: Exists is false until the transcript has been ingested.
type fleetSession struct {
	ID          string `json:"id"`
	Exists      bool   `json:"exists"`
	DisplayName string `json:"display_name,omitempty"`
	Project     string `json:"project,omitempty"`
	Machine     string `json:"machine,omitempty"`
}

type fleetStory struct {
	fleet.Story
	Sessions []fleetSession `json:"sessions"`
}

type fleetStoriesResponse struct {
	// Enabled is false when no fleet ledger is configured.
	Enabled bool         `json:"enabled"`
	Stories []fleetStory `json:"stories"`
	// Total is how many stories match the query across all pages.
	Total int `json:"total"`
	// TraceURLTemplate links a run's trace; {trace_id} is replaced.
	TraceURLTemplate string `json:"trace_url_template,omitempty"`
}

type fleetStoryResponse struct {
	Story            fleetStory `json:"story"`
	TraceURLTemplate string     `json:"trace_url_template,omitempty"`
}

// fleetLedger returns the configured ledger, opening it on first use. It
// returns nil, nil when the fleet is not configured.
func (s *Server) fleetLedger() (fleet.Ledger, error) {
	s.fleetMu.Lock()
	defer s.fleetMu.Unlock()
	if s.fleet != nil {
		return s.fleet, nil
	}
	s.mu.RLock()
	cfg := s.cfg.Fleet
	s.mu.RUnlock()
	if !cfg.Enabled() {
		return nil, nil
	}
	conn, err := fleet.OpenMySQL(cfg.DSN())
	if err != nil {
		return nil, err
	}
	l, err := fleet.NewSQLLedger(conn, cfg.Databases)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	s.fleet = l
	return l, nil
}

func (s *Server) fleetTraceURLTemplate() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Fleet.TraceURL
}

// joinSessions looks up each recorded session in the archive.
func (s *Server) joinSessions(ctx context.Context, st fleet.Story) (fleetStory, error) {
	out := fleetStory{Story: st, Sessions: make([]fleetSession, 0, len(st.SessionIDs))}
	for _, id := range st.SessionIDs {
		fs := fleetSession{ID: id}
		sess, err := s.db.GetSession(ctx, id)
		if err != nil {
			return out, err
		}
		if sess != nil {
			fs.Exists = true
			fs.Project, fs.Machine = sess.Project, sess.Machine
			if sess.DisplayName != nil {
				fs.DisplayName = *sess.DisplayName
			}
		}
		out.Sessions = append(out.Sessions, fs)
	}
	return out, nil
}

func (s *Server) humaListFleetStories(
	ctx context.Context,
	in *fleetStoriesInput,
) (*jsonOutput[fleetStoriesResponse], error) {
	l, err := s.fleetLedger()
	if err != nil {
		return nil, internalError("open fleet ledger", err)
	}
	resp := fleetStoriesResponse{Stories: []fleetStory{}, TraceURLTemplate: s.fleetTraceURLTemplate()}
	if l == nil {
		return &jsonOutput[fleetStoriesResponse]{Body: resp}, nil
	}
	resp.Enabled = true
	stories, total, err := l.Stories(ctx, fleet.StoryQuery{Search: in.Q, Offset: in.Offset, Limit: in.Limit})
	resp.Total = total
	if err != nil {
		return nil, internalError("list fleet stories", err)
	}
	for _, st := range stories {
		joined, err := s.joinSessions(ctx, st)
		if err != nil {
			return nil, internalError("join fleet sessions", err)
		}
		resp.Stories = append(resp.Stories, joined)
	}
	return &jsonOutput[fleetStoriesResponse]{Body: resp}, nil
}

func (s *Server) humaGetFleetStory(
	ctx context.Context,
	in *fleetStoryInput,
) (*jsonOutput[fleetStoryResponse], error) {
	l, err := s.fleetLedger()
	if err != nil {
		return nil, internalError("open fleet ledger", err)
	}
	if l == nil {
		return nil, apiErrorWithCode(http.StatusNotFound, "fleet_disabled", "fleet ledger not configured")
	}
	st, err := l.Story(ctx, in.ID)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil, apiError(http.StatusNotFound, "story not found")
	}
	if err != nil {
		return nil, internalError("get fleet story", err)
	}
	joined, err := s.joinSessions(ctx, *st)
	if err != nil {
		return nil, internalError("join fleet sessions", err)
	}
	return &jsonOutput[fleetStoryResponse]{Body: fleetStoryResponse{Story: joined, TraceURLTemplate: s.fleetTraceURLTemplate()}}, nil
}
