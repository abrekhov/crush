package dialog

import (
	"context"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/stretchr/testify/require"

	"github.com/abrekhov/crush/internal/session"
	"github.com/abrekhov/crush/internal/ui/common"
	"github.com/abrekhov/crush/internal/ui/styles"
	"github.com/abrekhov/crush/internal/workspace"
)

// runningWorkspace reports a fixed set of sessions as generating. The
// embedded interface panics on anything the dialog does not call.
type runningWorkspace struct {
	workspace.Workspace

	ready    bool
	sessions []session.Session
	busy     map[string]bool
}

func (w *runningWorkspace) AgentIsReady() bool { return w.ready }

func (w *runningWorkspace) AgentIsSessionBusy(id string) bool { return w.busy[id] }

func (w *runningWorkspace) ListSessions(context.Context) ([]session.Session, error) {
	return w.sessions, nil
}

func newRunningDialog(t *testing.T, ws *runningWorkspace, selected string) *Session {
	t.Helper()
	st := styles.CharmtonePantera()
	com := &common.Common{Workspace: ws, Styles: &st}
	s, err := NewSessions(com, selected)
	require.NoError(t, err)
	return s
}

func itemForID(t *testing.T, s *Session, id string) *SessionItem {
	t.Helper()
	for i := range s.list.Len() {
		item, ok := s.list.ItemAt(i).(*SessionItem)
		if ok && item.ID() == id {
			return item
		}
	}
	t.Fatalf("session %q not in the list", id)
	return nil
}

// A session that keeps generating after the user switches away is only
// visible in the session list, so it has to carry the running marker.
func TestSessionListMarksRunningSessions(t *testing.T) {
	t.Parallel()

	ws := &runningWorkspace{
		ready: true,
		sessions: []session.Session{
			{ID: "running", Title: "Background work"},
			{ID: "idle", Title: "Finished work"},
		},
		busy: map[string]bool{"running": true},
	}

	s := newRunningDialog(t, ws, "idle")

	require.True(t, itemForID(t, s, "running").Running(),
		"a generating session must be marked as running")
	require.False(t, itemForID(t, s, "idle").Running(),
		"an idle session must not be marked as running")
}

// The marker has to reach the rendered row; carrying the flag alone would
// leave the background run invisible.
func TestRunningMarkerIsRendered(t *testing.T) {
	t.Parallel()

	ws := &runningWorkspace{
		ready: true,
		sessions: []session.Session{
			{ID: "running", Title: "Background work"},
			{ID: "idle", Title: "Finished work"},
		},
		busy: map[string]bool{"running": true},
	}

	s := newRunningDialog(t, ws, "idle")

	require.Contains(t, itemForID(t, s, "running").Render(60), runningMarker,
		"the running session's row must show the marker")
	require.NotContains(t, itemForID(t, s, "idle").Render(60), runningMarker,
		"an idle session's row must stay unmarked")
}

// The marker shares the info column's width budget, so a row must still
// occupy exactly the width it was given.
func TestRunningMarkerKeepsRowWidth(t *testing.T) {
	t.Parallel()

	ws := &runningWorkspace{
		ready: true,
		sessions: []session.Session{
			{ID: "running", Title: strings.Repeat("long title ", 12)},
		},
		busy: map[string]bool{"running": true},
	}

	s := newRunningDialog(t, ws, "running")

	for _, width := range []int{20, 40, 80} {
		rendered := itemForID(t, s, "running").Render(width)
		for _, line := range strings.Split(rendered, "\n") {
			require.LessOrEqual(t, lipgloss.Width(line), width,
				"a marked row must not overflow width %d", width)
		}
	}
}

// Before the agent is up, probing session state is meaningless, so nothing
// may be reported as running.
func TestNoRunningMarkerBeforeAgentIsReady(t *testing.T) {
	t.Parallel()

	ws := &runningWorkspace{
		ready:    false,
		sessions: []session.Session{{ID: "running", Title: "Background work"}},
		busy:     map[string]bool{"running": true},
	}

	s := newRunningDialog(t, ws, "running")

	require.False(t, itemForID(t, s, "running").Running(),
		"nothing may be marked running before the agent is ready")
}

// Switching a session's running state has to invalidate the render cache,
// otherwise a finished run keeps showing the marker.
func TestSetRunningInvalidatesRenderCache(t *testing.T) {
	t.Parallel()

	ws := &runningWorkspace{
		ready:    true,
		sessions: []session.Session{{ID: "s", Title: "Work"}},
		busy:     map[string]bool{"s": true},
	}

	s := newRunningDialog(t, ws, "s")
	item := itemForID(t, s, "s")

	require.Contains(t, item.Render(60), runningMarker)

	item.SetRunning(false)
	require.NotContains(t, item.Render(60), runningMarker,
		"a finished run must drop the marker")
}
