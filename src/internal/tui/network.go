package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	networkinfo "github.com/ih0r-d/hostup/src/internal/network"
)

type networkState struct {
	loading, details bool
	report           networkinfo.Report
	cursor           int
	err              error
}
type networkMsg struct {
	id     int
	report networkinfo.Report
	err    error
}

func (m *model) showNetwork() tea.Cmd {
	m.enter(network)
	m.network = networkState{}
	return m.loadNetwork()
}
func (m *model) loadNetwork() tea.Cmd {
	m.network.loading, m.network.err, m.scroll = true, nil, 0
	ctx, cancel, id := m.request()
	inspect := m.deps.network
	return func() tea.Msg {
		defer cancel()
		if err := ctx.Err(); err != nil {
			return networkMsg{id: id, err: err}
		}
		report, err := inspect(ctx)
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return networkMsg{id, report, err}
	}
}
func (m *model) receiveNetwork(msg networkMsg) {
	if !m.opened || m.selected != network || msg.id != m.requestID {
		return
	}
	s := &m.network
	name := ""
	if s.cursor < len(s.report.Interfaces) {
		name = s.report.Interfaces[s.cursor].Name
	}
	s.loading, s.err, s.report = false, msg.err, msg.report
	s.cursor = 0
	found := false
	for i, iface := range s.report.Interfaces {
		if iface.Name == name {
			s.cursor = i
			found = true
			break
		}
	}
	if !found {
		s.details = false
	}
}
func (m *model) networkKey(key string) tea.Cmd {
	s := &m.network
	switch key {
	case "esc":
		if s.details {
			s.details = false
			m.scroll = 0
		} else {
			m.leaveSection()
		}
		return nil
	case "r":
		return m.loadNetwork()
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
		s.cursor = min(max(0, len(s.report.Interfaces)-1), s.cursor+1)
	case "enter":
		if s.err == nil && len(s.report.Interfaces) > 0 {
			s.details = true
			m.scroll = 0
		}
	}
	return nil
}

func (m *model) networkBody(t theme, width, height int) string {
	s := m.network
	if s.loading {
		return t.muted.Render("Reading local network configuration…")
	}
	if s.err != nil {
		if errors.Is(s.err, networkinfo.ErrUnsupportedPlatform) {
			return t.warning.Render("Network inspection unavailable") + "\n\nLinux is required."
		}
		return t.warning.Render("Network inspection unavailable") + "\n\n" + readError(s.err, "Could not inspect network configuration. Use hostup network status for details.")
	}
	if s.details && len(s.report.Interfaces) > 0 {
		return networkDetails(t, s.report, s.report.Interfaces[s.cursor])
	}
	lines := []string{t.muted.Render(fmt.Sprintf("%d interfaces · local snapshot", len(s.report.Interfaces)))}
	lines = append(lines, ansi.Truncate(networkDNS(s.report), width, "…"))
	defaults := 0
	for _, route := range s.report.Routes {
		if route.Default {
			defaults++
			if defaults <= 2 {
				lines = append(lines, ansi.Truncate("Default: "+networkRoute(route), width, "…"))
			}
		}
	}
	if defaults == 0 {
		lines = append(lines, "No default routes observed; see source status.")
	} else if defaults > 2 {
		lines = append(lines, "More default routes in interface details.")
	}
	problems := 0
	for _, source := range s.report.Sources {
		if source.Status != "available" {
			problems++
		}
	}
	if problems > 0 {
		lines = append(lines, t.warning.Render(fmt.Sprintf("%d source(s) incomplete · see CLI/details", problems)))
	}
	lines = append(lines, "")
	if len(s.report.Interfaces) == 0 {
		return strings.Join(append(lines, "No interfaces observed."), "\n")
	}
	if len(lines) > max(1, height-1) {
		lines = lines[:max(1, height-1)]
	}
	rows := max(1, height-len(lines))
	start := max(0, s.cursor-rows+1)
	for i := start; i < min(len(s.report.Interfaces), start+rows); i++ {
		iface := s.report.Interfaces[i]
		marker, style := "  ", t.text
		if i == s.cursor {
			marker, style = "> ", t.selected
		}
		text := fmt.Sprintf("%s%s · %s/%s · %d IPv4, %d IPv6", marker, safe(iface.Name), iface.AdministrativeState, iface.OperationalState, len(iface.IPv4), len(iface.IPv6))
		lines = append(lines, style.Render(ansi.Truncate(text, width, "…")))
	}
	return strings.Join(lines, "\n")
}

func networkDNS(r networkinfo.Report) string {
	servers := []string{}
	for _, server := range r.DNS.Nameservers {
		label := server.Address
		if server.LocalStub {
			label += " (local stub)"
		}
		servers = append(servers, label)
	}
	value := "none observed"
	if len(servers) > 0 {
		value = strings.Join(servers, ", ")
	}
	status := "unavailable"
	for _, source := range r.Sources {
		if source.Component == "dns" {
			status = source.Status
			break
		}
	}
	return "DNS: " + safe(value) + " · source " + status + " · upstream unknown"
}

func networkRoute(r networkinfo.Route) string {
	via := r.Gateway
	if via == "" {
		via = "-"
	}
	dev := r.Interface
	if dev == "" {
		dev = "unknown"
	}
	value := r.Destination + " via " + via + " dev " + dev + " (" + r.State + ")"
	if r.Source != "" {
		value += " from " + r.Source
	}
	if r.Metric != nil {
		value += fmt.Sprintf(" metric %d", *r.Metric)
	}
	return safe(value)
}

func networkDetails(t theme, r networkinfo.Report, iface networkinfo.Interface) string {
	mac := iface.MAC
	if mac == "" {
		mac = "not present"
	}
	lines := []string{t.title.Render(safe(iface.Name)), fmt.Sprintf("Index %d · %s · MTU %d", iface.Index, iface.Type, iface.MTU), "Admin: " + iface.AdministrativeState + " · Operational: " + iface.OperationalState, "MAC: " + mac}
	if iface.Loopback {
		lines = append(lines, "Loopback")
	}
	for _, group := range []struct {
		name  string
		items []string
	}{{"IPv4", iface.IPv4}, {"IPv6", iface.IPv6}} {
		lines = append(lines, "", t.accent.Render(group.name))
		if len(group.items) == 0 {
			lines = append(lines, "No addresses observed.")
		} else {
			for _, address := range group.items {
				lines = append(lines, safe(address))
			}
		}
	}
	lines = append(lines, "", networkDNS(r), "Search: "+safe(strings.Join(r.DNS.SearchDomains, ", ")), "Resolver: "+safe(r.DNS.Source))
	if r.DNS.ResolvedSource != "" {
		lines = append(lines, "Target: "+safe(r.DNS.ResolvedSource))
	}
	lines = append(lines, "", t.accent.Render("Observed interface routes"))
	count := 0
	for _, route := range r.Routes {
		if route.Interface == iface.Name {
			count++
			label := ""
			if route.Default {
				label = "default · "
			}
			lines = append(lines, label+networkRoute(route))
		}
	}
	if count == 0 {
		lines = append(lines, "No routes observed.")
	}
	for _, source := range r.Sources {
		if source.Status != "available" {
			lines = append(lines, "", t.warning.Render(safe(source.Component)+": "+source.Status), safe(source.Message))
		}
	}
	lines = append(lines, "", t.muted.Render("Local inspection only · no connectivity tests"))
	return strings.Join(lines, "\n")
}
