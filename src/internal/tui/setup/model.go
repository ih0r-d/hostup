// Package setupwizard provides the interactive presentation of the setup engine.
package setupwizard

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/ih0r-d/hostup/src/internal/setup"
)

type step int

const (
	welcome step = iota
	configure
	review
	applying
	results
)

type loadedMsg struct {
	plan setup.Plan
	err  error
}

type plannedMsg struct {
	id   int
	plan setup.Plan
	err  error
}

type appliedMsg struct {
	result setup.Result
	err    error
}

type cancelledMsg struct{ err error }

// One ordered queue carries progress and completion. Its bounded buffer holds
// all events for the two supported operations, even if the renderer stops.
type application struct {
	events  chan tea.Msg
	done    chan struct{}
	started atomic.Bool
	result  setup.Result
	err     error
}

func (a *application) next() tea.Msg { return <-a.events }

type model struct {
	engine          *setup.Engine
	ctx             context.Context
	cancel          context.CancelFunc
	page            step
	inputs          [2]textinput.Model
	current         setup.Configuration
	plan            setup.Plan
	result          setup.Result
	operationStates [2]string
	application     *application
	focus           int
	width, height   int
	scroll          int
	loaded          bool
	planning        bool
	planCancel      context.CancelFunc
	requestID       int
	confirmed       bool
	cancelling      bool
	noColor         bool
	profile         colorprofile.Profile
	logo            string
	details         bool
	err             error
	notice          string
	platform        string
}

func newModel(ctx context.Context, engine *setup.Engine, noColor bool) *model {
	ctx, cancel := context.WithCancel(ctx)
	m := &model{engine: engine, ctx: ctx, cancel: cancel, width: 80, height: 24,
		noColor: noColor, profile: colorprofile.TrueColor, platform: runtime.GOOS + "/" + runtime.GOARCH}
	for i := range m.inputs {
		m.inputs[i] = textinput.New()
		m.inputs[i].Prompt = ""
		m.inputs[i].CharLimit = 256
		// Steady cursors and terminal-default text avoid animation and theme contrast issues.
		m.inputs[i].SetStyles(textinput.Styles{})
		m.inputs[i].SetWidth(m.contentWidth() - 3)
	}
	return m
}

func (m *model) Init() tea.Cmd {
	commands := []tea.Cmd{func() tea.Msg {
		plan, err := m.engine.Plan(m.ctx, setup.Desired{})
		return loadedMsg{plan, err}
	}, func() tea.Msg {
		<-m.ctx.Done()
		return cancelledMsg{m.ctx.Err()}
	}}
	return tea.Batch(commands...)
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.ColorProfileMsg:
		m.profile = msg.Profile
		m.logo = ""
		m.prepareLogo()
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.prepareLogo()
		for i := range m.inputs {
			m.inputs[i].SetWidth(max(1, m.contentWidth()-3))
		}
		if m.page == configure {
			m.scroll = m.focusOffset()
		}
	case loadedMsg:
		if m.page != welcome || m.cancelling {
			return m, nil
		}
		m.loaded, m.err = true, msg.err
		if msg.err == nil {
			m.current = setup.Configuration{Hostname: msg.plan.Hostname.Current, Timezone: msg.plan.Timezone.Current}
			m.inputs[0].SetValue(m.current.Hostname)
			m.inputs[1].SetValue(m.current.Timezone)
		}
	case plannedMsg:
		if !m.planning || msg.id != m.requestID {
			return m, nil // Ignore a response from a review the user left.
		}
		m.planning, m.err = false, msg.err
		if msg.err != nil {
			m.notice = "Could not refresh the plan. Go back or cancel."
			return m, nil
		}
		// Refresh fields the user did not edit, preserving explicit edits.
		if m.inputs[0].Value() == m.current.Hostname {
			m.inputs[0].SetValue(msg.plan.Hostname.Current)
		}
		if m.inputs[1].Value() == m.current.Timezone {
			m.inputs[1].SetValue(msg.plan.Timezone.Current)
		}
		m.current = setup.Configuration{Hostname: msg.plan.Hostname.Current, Timezone: msg.plan.Timezone.Current}
		m.plan, m.page, m.confirmed, m.scroll, m.notice = msg.plan, review, false, 0, ""
	case setup.Progress:
		if m.page == applying {
			for i, name := range []string{"hostname", "timezone"} {
				if msg.Setting == name {
					m.operationStates[i] = string(msg.State)
				}
			}
			return m, m.application.next
		}
	case appliedMsg:
		m.result, m.err, m.page, m.scroll = msg.result, msg.err, results, 0
		if m.cancelling && m.err == nil {
			// Completion can race a cancel request. Keep the actual results, but do
			// not turn the user's cancellation into an unqualified success message.
			m.err = context.Canceled
		}
		if !m.plan.HasChanges() && m.err == nil {
			m.cancel()
			return m, tea.Quit // Done on a no-op review only verifies the snapshot.
		}
	case cancelledMsg:
		return m, m.cancelSetup(msg.err)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.cancelSetup(context.Canceled)
		}
		if m.width < minimumWidth || m.height < minimumHeight {
			return m, nil // Never apply while the review/controls cannot be displayed.
		}
		if msg.String() == "pgdown" || msg.String() == "pgup" {
			amount := max(1, m.height/2)
			if msg.String() == "pgup" {
				amount = -amount
			}
			m.scroll = max(0, m.scroll+amount)
			return m, nil
		}
		if m.page != configure && msg.String() == "d" && m.err != nil {
			m.details = !m.details
			return m, nil
		}
		return m, m.key(msg)
	default:
		// Bracketed paste and clipboard responses are not key-press messages.
		if m.page == configure && !m.planning {
			var cmd tea.Cmd
			m.inputs[m.focus], cmd = m.inputs[m.focus].Update(message)
			m.validateFields()
			m.scroll = m.focusOffset()
			return m, cmd
		}
	}
	return m, nil
}

func (m *model) key(msg tea.KeyPressMsg) tea.Cmd {
	switch m.page {
	case welcome:
		switch msg.String() {
		case "esc":
			return m.cancelSetup(context.Canceled)
		case "enter":
			if m.loaded && m.err == nil {
				m.page, m.scroll = configure, 0
				return m.focusInput(m.focus)
			}
			if m.loaded && m.err != nil {
				m.cancel()
				return tea.Quit
			}
		}
	case configure:
		if msg.String() == "esc" {
			if m.planning {
				m.planCancel()
				m.planning, m.notice = false, ""
				m.requestID++
			} else {
				m.page, m.scroll = welcome, 0
				m.err, m.notice = nil, ""
				m.inputs[m.focus].Blur()
			}
			return nil
		}
		if m.planning {
			return nil
		}
		switch msg.String() {
		case "tab", "shift+tab", "up", "down":
			focus := 1 - m.focus
			if msg.String() == "up" {
				focus = 0
			} else if msg.String() == "down" {
				focus = 1
			}
			return m.focusInput(focus)
		case "enter":
			m.validateFields()
			for i := range m.inputs {
				if m.inputs[i].Err != nil {
					return m.focusInput(i)
				}
			}
			return m.prepareReview()
		default:
			var cmd tea.Cmd
			m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
			m.validateFields()
			m.scroll = m.focusOffset()
			m.notice = ""
			return cmd
		}
	case review:
		switch msg.String() {
		case "esc":
			m.page, m.confirmed, m.notice = configure, false, ""
			return m.focusInput(m.focus)
		case "space":
			if m.plan.HasChanges() {
				m.confirmed, m.notice = !m.confirmed, ""
			}
		case "enter":
			if m.plan.HasChanges() && !m.confirmed {
				m.notice = "Confirm the changes with Space."
				return nil
			}
			return m.beginApply()
		}
	case results:
		if msg.String() == "enter" || msg.String() == "esc" {
			m.cancel()
			return tea.Quit
		}
	}
	return nil
}

func (m *model) focusInput(index int) tea.Cmd {
	m.inputs[m.focus].Blur()
	m.focus = index
	m.scroll = m.focusOffset()
	return m.inputs[index].Focus()
}

func (m *model) validateFields() {
	for i, item := range []struct {
		current  string
		validate func(string) error
	}{{m.current.Hostname, setup.ValidateHostname}, {m.current.Timezone, setup.ValidateTimezone}} {
		m.inputs[i].Err = nil
		if value := m.inputs[i].Value(); value != item.current {
			m.inputs[i].Err = item.validate(value)
		}
	}
}

func (m *model) prepareReview() tea.Cmd {
	desired := setup.Desired{}
	hostname, timezone := m.inputs[0].Value(), m.inputs[1].Value()
	if hostname != m.current.Hostname {
		desired.Hostname = &hostname
	}
	if timezone != m.current.Timezone {
		desired.Timezone = &timezone
	}
	m.planning, m.notice = true, "Reading a fresh plan…"
	m.requestID++
	id := m.requestID
	ctx, cancel := context.WithCancel(m.ctx)
	m.planCancel = cancel
	return func() tea.Msg {
		defer cancel()
		plan, err := m.engine.Plan(ctx, desired)
		return plannedMsg{id, plan, err}
	}
}

func (m *model) beginApply() tea.Cmd {
	m.page, m.scroll, m.notice = applying, 0, ""
	m.inputs[m.focus].Blur()
	m.operationStates = [2]string{"waiting", "waiting"}
	a := &application{events: make(chan tea.Msg, 8), done: make(chan struct{})}
	m.application = a
	engine, ctx, plan := m.engine, m.ctx, m.plan
	return tea.Batch(func() tea.Msg {
		a.started.Store(true)
		defer close(a.done)
		a.result, a.err = engine.ApplyWithProgress(ctx, plan, func(event setup.Progress) { a.events <- event })
		a.events <- appliedMsg{a.result, a.err}
		return nil
	}, a.next)
}

func (m *model) cancelSetup(err error) tea.Cmd {
	if m.page == results {
		m.cancel()
		return tea.Quit
	}
	m.cancelling = true
	m.cancel()
	if m.page == applying {
		return nil // Wait for the engine's final result; never discard partial work.
	}
	m.err = errors.Join(m.err, err)
	m.page = results
	return tea.Quit
}
