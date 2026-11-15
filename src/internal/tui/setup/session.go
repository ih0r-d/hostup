package setupwizard

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/ih0r-d/hostup/src/internal/setup"
)

// Session embeds the existing wizard in a parent's event loop. It owns no
// terminal or Program; standalone Run continues to use the original model.
type Session struct{ model *model }

// FinishedMsg closes this wizard session, never its parent application.
type FinishedMsg struct{ Session *Session }

type sessionMsg struct {
	session *Session
	message tea.Msg
}

func NewSession(ctx context.Context, engine *setup.Engine, noColor bool) *Session {
	return &Session{model: newModel(ctx, engine, noColor)}
}

func (s *Session) Init() tea.Cmd  { return s.command(s.model.Init()) }
func (s *Session) View() tea.View { return s.model.View() }
func (s *Session) Applying() bool { return s.model.page == applying }

// Summary remains available after Close, including partial or cancelled work.
func (s *Session) Summary() string { return s.model.withDetails(s.model.summary()) }

// Close cancels pending work and joins any application, preserving partial
// results even if the parent's renderer/input failed during a system change.
func (s *Session) Close(err error) { s.model.stop(err) }

func (s *Session) Update(message tea.Msg) tea.Cmd {
	if msg, ok := message.(sessionMsg); ok {
		if msg.session != s {
			return nil // A response from a previous session must not reach this one.
		}
		message = msg.message
	}
	_, command := s.model.Update(message)
	return s.command(command)
}

// Tag wizard-owned async results and intercept Quit before Bubble Tea sees it.
// Terminal requests remain native messages, so the parent Program handles them.
func (s *Session) command(command tea.Cmd) tea.Cmd {
	if command == nil {
		return nil
	}
	return func() tea.Msg {
		switch msg := command().(type) {
		case tea.BatchMsg:
			commands := make([]tea.Cmd, len(msg))
			for i, child := range msg {
				commands[i] = s.command(child)
			}
			return tea.BatchMsg(commands)
		case tea.QuitMsg:
			return FinishedMsg{s}
		case loadedMsg, plannedMsg, appliedMsg, cancelledMsg, setup.Progress:
			return sessionMsg{s, msg}
		default:
			return msg
		}
	}
}
