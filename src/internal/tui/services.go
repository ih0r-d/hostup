package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/system"
)

type servicesState struct {
	loading, details bool
	items            []system.Service
	cursor           int
	detail           system.Service
	err              error
}

type servicesMsg struct {
	id      int
	items   []system.Service
	detail  system.Service
	details bool
	err     error
}

func (m *model) showServices() tea.Cmd {
	m.enter(services)
	m.services = servicesState{loading: true}
	ctx, cancel, id := m.request()
	manager := m.deps.services
	return func() tea.Msg {
		defer cancel()
		items, err := manager.List(ctx)
		return servicesMsg{id: id, items: items, err: err}
	}
}

func (m *model) receiveServices(msg servicesMsg) {
	if !m.opened || m.selected != services || msg.id != m.requestID {
		return
	}
	m.services.loading, m.services.err = false, msg.err
	if msg.details {
		m.services.detail = msg.detail
	} else {
		m.services.items, m.services.cursor = msg.items, 0
	}
}

func (m *model) servicesKey(key string) tea.Cmd {
	s := &m.services
	if key == "esc" {
		if s.details {
			m.stopRequest()
			s.details, s.loading, s.err = false, false, nil
			m.scroll = 0
		} else {
			m.leaveSection()
		}
		return nil
	}
	if s.loading {
		return nil
	}
	if s.details {
		switch key {
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll = min(m.scroll+1, m.maxScroll())
		}
		return nil
	}
	switch key {
	case "up", "k":
		s.cursor = max(0, s.cursor-1)
	case "down", "j":
		s.cursor = min(max(0, len(s.items)-1), s.cursor+1)
	case "enter":
		if s.err == nil && len(s.items) > 0 {
			s.details, s.loading, s.err = true, true, nil
			m.scroll = 0
			ctx, cancel, id := m.request()
			name, manager := s.items[s.cursor].Name, m.deps.services
			return func() tea.Msg {
				defer cancel()
				detail, err := manager.StatusInfo(ctx, name)
				return servicesMsg{id: id, detail: detail, details: true, err: err}
			}
		}
	}
	return nil
}

func (m *model) servicesBody(t theme, width, height int) string {
	s := m.services
	if s.loading {
		if s.details {
			return t.muted.Render("Reading service status…")
		}
		return t.muted.Render("Reading services…")
	}
	if s.err != nil {
		return t.warning.Render("Services unavailable") + "\n\n" +
			readError(s.err, "Could not read services. Use hostup service list/status for details.")
	}
	if s.details {
		return t.title.Render(safe(s.detail.Name)) + "\n\n" +
			"Load state    " + safe(s.detail.Load) + "\n\n" +
			"Active state  " + t.serviceState(s.detail.Active) + "\n\n" +
			"Substate      " + safe(s.detail.Sub)
	}
	if len(s.items) == 0 {
		return t.muted.Render("No services found")
	}
	// Render only visible rows, even for thousands of units. The selected row
	// stays visible after resizing without mutating state from View.
	rows := max(1, height-2)
	if width < 62 {
		rows = max(1, rows/2)
	}
	start := max(0, s.cursor-rows+1)
	end := min(len(s.items), start+rows)
	lines := []string{t.muted.Render(fmt.Sprintf("%d services · %d/%d", len(s.items), s.cursor+1, len(s.items)))}
	if width >= 62 {
		lines = append(lines, t.muted.Render(fmt.Sprintf("  %-*s %-10s %-10s %s", width-36, "UNIT", "LOAD", "ACTIVE", "SUB")))
	} else {
		lines = append(lines, t.muted.Render("UNIT · LOAD / ACTIVE / SUB"))
	}
	for i := start; i < end; i++ {
		item := s.items[i]
		marker, style := "  ", t.text
		if i == s.cursor {
			marker, style = "> ", t.selected
		}
		var row string
		if width >= 62 {
			nameWidth := width - 36
			row = fmt.Sprintf("%s%-*s %-10s %-10s %s", marker, nameWidth,
				ansi.Truncate(safe(item.Name), nameWidth, "…"), safe(item.Load), safe(item.Active), safe(item.Sub))
		} else {
			row = ansi.Truncate(marker+safe(item.Name), width, "…") + "\n" +
				ansi.Truncate("  "+safe(item.Load)+" / "+safe(item.Active)+" / "+safe(item.Sub), width, "…")
		}
		lines = append(lines, style.Render(row))
	}
	return strings.Join(lines, "\n")
}
