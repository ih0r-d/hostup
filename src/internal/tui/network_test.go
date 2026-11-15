package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	networkinfo "github.com/ih0r-d/hostup/src/internal/network"
)

func networkReport() networkinfo.Report {
	metric := uint32(100)
	return networkinfo.Report{Interfaces: []networkinfo.Interface{{Name: "ens3", Index: 2, Type: "ethernet", AdministrativeState: "up", OperationalState: "up", MTU: 1500, IPv4: []string{"192.0.2.9/24"}, IPv6: []string{"2001:db8::9/64"}}}, Routes: []networkinfo.Route{{Family: "ipv4", Destination: "0.0.0.0/0", Gateway: "192.0.2.1", Interface: "ens3", Default: true, State: "active", Metric: &metric}}, DNS: networkinfo.DNS{Source: "/etc/resolv.conf", Nameservers: []networkinfo.Nameserver{{Address: "127.0.0.53", LocalStub: true}}, SearchDomains: []string{"example.test"}, UpstreamStatus: "unknown"}, Sources: []networkinfo.Source{{Component: "dns", Status: "available"}}}
}

func TestNetworkLoadSelectionDetailsAndRefresh(t *testing.T) {
	m, _ := testModel(t)
	calls := 0
	m.deps.network = func(ctx context.Context) (networkinfo.Report, error) {
		calls++
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		return networkReport(), nil
	}
	m.selected = network
	command := press(m, "enter")
	if !m.network.loading || calls != 0 || !strings.Contains(m.body(m.theme(), 70, 20), "Reading local") {
		t.Fatal("load blocked event loop")
	}
	deliver(m, command)
	if calls != 1 || !strings.Contains(m.body(m.theme(), 70, 20), "> ens3") {
		t.Fatal("missing interface")
	}
	press(m, "enter")
	body := ansi.Strip(m.body(m.theme(), 70, 30))
	for _, value := range []string{"192.0.2.9/24", "2001:db8::9/64", "192.0.2.1", "local stub", "upstream unknown", "MTU 1500"} {
		if !strings.Contains(body, value) {
			t.Fatalf("missing %s: %s", value, body)
		}
	}
	deliver(m, press(m, "r"))
	if calls != 2 || !m.network.details {
		t.Fatal("refresh lost selected details")
	}
	press(m, "esc")
	if m.network.details || !m.opened {
		t.Fatal("details Esc")
	}
	press(m, "esc")
	if m.opened {
		t.Fatal("list Esc")
	}
}

func TestNetworkEmptyFailureAndPartialResults(t *testing.T) {
	for _, tt := range []struct {
		name string
		r    networkinfo.Report
		err  error
		want string
	}{
		{"empty", networkinfo.Report{}, nil, "No interfaces observed"},
		{"failure", networkinfo.Report{}, errors.New("native failure"), "Network inspection unavailable"},
		{"unsupported", networkinfo.Report{}, networkinfo.ErrUnsupportedPlatform, "Linux is required"},
		{"partial", networkinfo.Report{Sources: []networkinfo.Source{{Component: "dns", Status: "unavailable", Message: "file missing"}}}, nil, "source(s) incomplete"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := testModel(t)
			m.deps.network = func(context.Context) (networkinfo.Report, error) { return tt.r, tt.err }
			deliver(m, m.showNetwork())
			body := m.body(m.theme(), 70, 20)
			if !strings.Contains(body, tt.want) {
				t.Fatal(body)
			}
			if command := press(m, "enter"); command != nil || m.network.details {
				t.Fatal("empty selection opened details")
			}
		})
	}
}

func TestNetworkScrollResizeAndStaleRefresh(t *testing.T) {
	m, _ := testModel(t)
	r := networkReport()
	for i := 3; i < 100; i++ {
		iface := r.Interfaces[0]
		iface.Name = fmt.Sprintf("veth%d", i)
		iface.Index = i
		r.Interfaces = append(r.Interfaces, iface)
	}
	m.deps.network = func(context.Context) (networkinfo.Report, error) { return r, nil }
	deliver(m, m.showNetwork())
	for i := 0; i < 40; i++ {
		press(m, "j")
	}
	press(m, "k")
	press(m, "down")
	press(m, "up")
	press(m, "j")
	for _, size := range [][2]int{{120, 32}, {80, 24}, {32, 14}, {24, 12}, {12, 4}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("width overflow")
			}
		}
		if strings.Count(view, "\n")+1 > size[1] {
			t.Fatal("height overflow")
		}
		if size[0] >= 24 && !strings.Contains(ansi.Strip(view), "> veth") {
			t.Fatal("selected interface clipped", ansi.Strip(view))
		}
	}
	command := press(m, "r")
	id := m.requestID
	press(m, "esc")
	deliver(m, command)
	if m.opened {
		t.Fatal("late response reopened screen")
	}
	m.showNetwork()
	m.Update(networkMsg{id: id, report: r})
	if !m.network.loading {
		t.Fatal("old refresh replaced new request")
	}
}

func TestNetworkCancelInFlightAndNoPolling(t *testing.T) {
	m, _ := testModel(t)
	entered := make(chan struct{})
	done := make(chan tea.Msg, 1)
	m.deps.network = func(ctx context.Context) (networkinfo.Report, error) {
		close(entered)
		<-ctx.Done()
		return networkinfo.Report{}, ctx.Err()
	}
	command := m.showNetwork()
	go func() { done <- command() }()
	<-entered
	press(m, "esc")
	m.Update(<-done)
	if m.opened {
		t.Fatal("cancelled request reopened screen")
	}
	calls := 0
	m.deps.network = func(context.Context) (networkinfo.Report, error) { calls++; return networkReport(), nil }
	deliver(m, m.showNetwork())
	m.View()
	m.View()
	press(m, "tab")
	press(m, "j")
	if calls != 1 {
		t.Fatal("render/navigation polls the backend")
	}
}

func TestNetworkRefreshHandlesRemovedSelection(t *testing.T) {
	m, _ := testModel(t)
	r := networkReport()
	other := r.Interfaces[0]
	other.Name = "ens4"
	other.Index = 4
	r.Interfaces = append(r.Interfaces, other)
	m.deps.network = func(context.Context) (networkinfo.Report, error) { return r, nil }
	deliver(m, m.showNetwork())
	press(m, "j")
	press(m, "enter")
	if !m.network.details || m.network.cursor != 1 {
		t.Fatal("interface selection")
	}
	r.Interfaces = r.Interfaces[:1]
	deliver(m, press(m, "r"))
	if m.network.details || m.network.cursor != 0 {
		t.Fatal("removed interface silently replaced in details")
	}
}
