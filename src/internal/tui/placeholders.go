package tui

import tea "charm.land/bubbletea/v2"

// Explicit module entry points keep navigation independent from future module
// interactions. Unfinished sections perform no backend work.
func (m *model) showNetwork() tea.Cmd  { return m.showPlaceholder(network) }
func (m *model) showVPN() tea.Cmd      { return m.showPlaceholder(vpn) }
func (m *model) showSecurity() tea.Cmd { return m.showPlaceholder(security) }
func (m *model) showStorage() tea.Cmd  { return m.showPlaceholder(storage) }
func (m *model) showPackages() tea.Cmd { return m.showPlaceholder(packages) }
func (m *model) showLogs() tea.Cmd     { return m.showPlaceholder(logs) }

func (m *model) showPlaceholder(s section) tea.Cmd {
	m.enter(s)
	return nil
}

func (m *model) placeholderBody(t theme) string {
	body := t.title.Render("Not implemented yet")
	if m.selected == vpn {
		body += "\n\n" + t.muted.Render("Planned providers") +
			"\n\nWireGuard\nTailscale\nHeadscale\nNetBird\nZeroTier\nOpenVPN"
	}
	return body
}
