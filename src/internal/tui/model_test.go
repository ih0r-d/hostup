package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/setup"
	"github.com/ih0r-d/hostup/src/internal/system"
	setupwizard "github.com/ih0r-d/hostup/src/internal/tui/setup"
)

type serviceFixture struct {
	output, status string
	err            error
	calls          []string
	block          bool
	entered        chan struct{}
}

func (f *serviceFixture) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name != "systemctl" || (args[0] != "list-units" && args[0] != "show") {
		return nil, errors.New("unexpected state-changing operation")
	}
	f.calls = append(f.calls, strings.Join(args, " "))
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New("missing read deadline")
	}
	if f.block {
		close(f.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if args[0] == "show" {
		return []byte(f.status), f.err
	}
	return []byte(f.output), f.err
}

func (*serviceFixture) Stream(context.Context, io.Writer, string, ...string) error {
	return errors.New("TUI must never stream logs")
}

func report() system.Diagnostics {
	return system.Diagnostics{
		Platform: system.Platform{OS: "linux", Hostname: "hostup-sandbox", Distribution: "ubuntu", Version: "24.04", Architecture: "arm64"},
		CPU:      system.CPUInfo{LogicalCPUs: 4},
		Memory:   system.MemoryInfo{TotalBytes: 8 << 30, AvailableBytes: 3 << 30},
		Disk:     system.DiskInfo{Path: "/", TotalBytes: 64 << 30, UsedBytes: 20 << 30},
	}
}

func testModel(t *testing.T) (*model, *serviceFixture) {
	t.Helper()
	f := &serviceFixture{output: "ssh.service loaded active running SSH\ncron.service loaded inactive dead Cron\n",
		status: "Id=ssh.service\nLoadState=loaded\nActiveState=inactive\nSubState=dead\n"}
	engine := setup.NewEngine(func(context.Context) (setup.Configuration, error) {
		return setup.Configuration{Hostname: "hostup-sandbox", Timezone: "UTC"}, nil
	},
		func(context.Context, string, ...string) ([]byte, error) {
			t.Error("unintended setup change")
			return nil, errors.New("forbidden")
		})
	m := newModel(context.Background(), dependencies{func() (system.Diagnostics, error) { return report(), nil }, system.NewServiceManager(f), engine}, true)
	t.Cleanup(func() { m.close(nil) })
	return m, f
}

func press(m *model, key string) tea.Cmd {
	var k tea.Key
	switch key {
	case "enter":
		k.Code = tea.KeyEnter
	case "esc":
		k.Code = tea.KeyEscape
	case "up":
		k.Code = tea.KeyUp
	case "down":
		k.Code = tea.KeyDown
	case "tab":
		k.Code = tea.KeyTab
	case "ctrl+c":
		k.Code, k.Mod = 'c', tea.ModCtrl
	default:
		k.Code, k.Text = []rune(key)[0], key
	}
	_, command := m.Update(tea.KeyPressMsg(k))
	return command
}

func deliver(m *model, command tea.Cmd) tea.Cmd {
	if command == nil {
		return nil
	}
	_, next := m.Update(command())
	return next
}

func isQuit(command tea.Cmd) bool {
	if command == nil {
		return false
	}
	_, ok := command().(tea.QuitMsg)
	return ok
}

func TestNavigation(t *testing.T) {
	m, f := testModel(t)
	reads := 0
	m.deps.diagnose = func() (system.Diagnostics, error) { reads++; return report(), nil }
	if m.selected != overview || m.opened {
		t.Fatal("initial navigation is not Overview")
	}
	want := []string{"Overview", "System Setup", "Services", "Network", "VPN", "Security", "Storage", "Packages", "Logs"}
	if !reflect.DeepEqual(sectionNames[:], want) {
		t.Fatal(sectionNames)
	}
	for i := range want {
		if int(m.selected) != i {
			t.Fatalf("selection=%v want=%d", m.selected, i)
		}
		if command := press(m, "tab"); command != nil || int(m.selected) != i {
			t.Fatal("Tab moved single-control focus")
		}
		key := "j"
		if i%2 == 0 {
			key = "down"
		}
		press(m, key)
	}
	for i := len(want) - 1; i >= 0; i-- {
		if int(m.selected) != i {
			t.Fatal("up navigation")
		}
		key := "k"
		if i%2 == 0 {
			key = "up"
		}
		press(m, key)
	}
	if reads != 0 || len(f.calls) != 0 {
		t.Fatal("navigation triggered backend reads")
	}
	if !isQuit(press(m, "q")) {
		t.Fatal("q did not exit navigation")
	}
}

func TestSectionEntriesAndPlaceholders(t *testing.T) {
	for _, s := range []section{overview, services, network, vpn, security, storage, packages, logs} {
		t.Run(sectionNames[s], func(t *testing.T) {
			m, f := testModel(t)
			m.selected = s
			command := press(m, "enter")
			if !m.opened || m.selected != s {
				t.Fatal("section not opened")
			}
			if s == overview || s == services {
				if command == nil {
					t.Fatal("missing load")
				}
				deliver(m, command)
			} else {
				if command != nil {
					t.Fatal("placeholder runs work")
				}
				plain := ansi.Strip(m.body(m.theme(), 70, 24))
				if !strings.Contains(plain, "Not implemented yet") {
					t.Fatal(plain)
				}
				if s == vpn {
					for _, provider := range []string{"WireGuard", "Tailscale", "Headscale", "NetBird", "ZeroTier", "OpenVPN"} {
						if !strings.Contains(plain, provider) {
							t.Fatal(provider)
						}
					}
				}
				if len(f.calls) != 0 {
					t.Fatal("placeholder called backend")
				}
			}
			if isQuit(press(m, "q")) {
				t.Fatal("q exits outside navigation")
			}
			press(m, "esc")
			if m.opened {
				t.Fatal("Esc did not return to navigation")
			}
		})
	}
	// Independently callable entry functions exist for every unfinished module.
	m, _ := testModel(t)
	for _, entry := range []func() tea.Cmd{m.showNetwork, m.showVPN, m.showSecurity, m.showStorage, m.showPackages, m.showLogs} {
		if entry() != nil || !strings.Contains(m.body(m.theme(), 70, 24), "Not implemented yet") {
			t.Fatal("placeholder entry")
		}
	}
}

func TestOverviewStatesAndStaleResults(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		text string
	}{
		{"success", nil, "Diagnostics collected"},
		{"failure", errors.New("raw diagnostic output\x1b[31m"), "Diagnostics unavailable"},
		{"unsupported", errors.New("unsupported OS: darwin"), "Linux is required"},
		{"timeout", context.DeadlineExceeded, "timed out"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := testModel(t)
			m.deps.diagnose = func() (system.Diagnostics, error) { return report(), tt.err }
			command := m.showOverview()
			if !m.overview.loading || !strings.Contains(m.body(m.theme(), 70, 24), "Reading") {
				t.Fatal("no loading state")
			}
			deliver(m, command)
			body := ansi.Strip(m.body(m.theme(), 70, 30))
			if m.overview.loading || !strings.Contains(body, tt.text) || strings.Contains(body, "raw diagnostic") {
				t.Fatal(body)
			}
			if tt.err == nil {
				for _, value := range []string{"hostup-sandbox", "ubuntu 24.04", "arm64", "4", "3.00 GiB", "8.00 GiB", "20.00 GiB", "64.00 GiB"} {
					if !strings.Contains(body, value) {
						t.Fatalf("missing %s: %s", value, body)
					}
				}
			}
		})
	}
	m, _ := testModel(t)
	command := m.showOverview()
	id := m.requestID
	press(m, "esc")
	deliver(m, command)
	if m.overview.report.Platform.Hostname != "" {
		t.Fatal("stale result accepted")
	}
	m.showOverview()
	m.Update(overviewMsg{id: id, report: report()})
	if !m.overview.loading {
		t.Fatal("previous request replaced new request")
	}
}

func TestServicesListDetailsAndScrolling(t *testing.T) {
	m, f := testModel(t)
	var lines strings.Builder
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&lines, "unit-%04d.service loaded active running Unit\n", i)
	}
	f.output = lines.String()
	command := m.showServices()
	if !m.services.loading || !strings.Contains(m.body(m.theme(), 70, 24), "Reading services") {
		t.Fatal("no loading")
	}
	deliver(m, command)
	for i := 0; i < 70; i++ {
		press(m, "j")
	}
	if m.services.cursor != 70 {
		t.Fatal(m.services.cursor)
	}
	_, height := m.contentSize()
	body := ansi.Strip(m.servicesBody(m.theme(), 70, height))
	if !strings.Contains(body, "> unit-0070.service") || strings.Contains(body, "unit-0000.service") || strings.Count(body, "unit-") > height {
		t.Fatal(body)
	}
	press(m, "up")
	press(m, "down")
	press(m, "k")
	press(m, "j")
	command = press(m, "enter")
	if !m.services.details || !m.services.loading {
		t.Fatal("detail not loading")
	}
	deliver(m, command)
	body = ansi.Strip(m.body(m.theme(), 70, 24))
	for _, value := range []string{"ssh.service", "loaded", "inactive", "dead"} {
		if !strings.Contains(body, value) {
			t.Fatal(body)
		}
	}
	if len(f.calls) != 2 || !strings.HasPrefix(f.calls[1], "show ") || !strings.HasSuffix(f.calls[1], "unit-0070.service") {
		t.Fatal(f.calls)
	}
	press(m, "esc")
	if m.services.details || m.services.cursor != 70 || len(f.calls) != 2 {
		t.Fatal("detail back lost list state or reloaded")
	}
	press(m, "esc")
	if m.opened {
		t.Fatal("list back")
	}
}

func TestServicesEmptyErrorsAndCancelledReads(t *testing.T) {
	for _, tt := range []struct {
		name, output string
		err          error
		want         string
	}{
		{"empty", "", nil, "No services found"},
		{"failure", "", errors.New("raw systemctl output"), "Services unavailable"},
		{"unsupported", "", errors.New("service management is unsupported on darwin"), "Linux is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, f := testModel(t)
			f.output, f.err = tt.output, tt.err
			deliver(m, m.showServices())
			body := m.body(m.theme(), 70, 24)
			if !strings.Contains(body, tt.want) || strings.Contains(body, "raw systemctl") {
				t.Fatal(body)
			}
			if press(m, "enter") != nil {
				t.Fatal("empty/error opened a service")
			}
		})
	}
	m, f := testModel(t)
	deliver(m, m.showServices())
	f.err = errors.New("show failed")
	deliver(m, press(m, "enter"))
	if m.services.err == nil {
		t.Fatal("detail error lost")
	}
	press(m, "esc")
	if m.services.err != nil || m.services.details {
		t.Fatal("detail error leaked into list")
	}
	f.block = true
	f.entered = make(chan struct{})
	command := m.showServices()
	done := make(chan tea.Msg, 1)
	go func() { done <- command() }()
	<-f.entered
	press(m, "esc")
	select {
	case msg := <-done:
		m.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("read was not cancelled")
	}
	if m.opened {
		t.Fatal("cancelled response reopened screen")
	}
}

func TestWizardCompositionAndReturn(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprint(cancel), func(t *testing.T) {
			m, _ := testModel(t)
			m.selected = systemSetup
			initial := press(m, "enter")
			if m.wizard == nil || initial == nil {
				t.Fatal("wizard not opened")
			}
			// Only run the initial read; its sibling command watches cancellation.
			batch := initial().(tea.BatchMsg)
			deliver(m, batch[0])
			if !strings.Contains(ansi.Strip(m.View().Content), "Make this host yours.") {
				t.Fatal("existing wizard not rendered")
			}
			if cancel {
				deliver(m, press(m, "esc"))
			} else {
				press(m, "enter")             // configure
				press(m, "tab")               // existing wizard owns multi-field focus
				deliver(m, press(m, "enter")) // read-only review
				if !strings.Contains(ansi.Strip(m.View().Content), "Already configured") {
					t.Fatal("wizard no-op review missing")
				}
				apply := press(m, "enter")().(tea.BatchMsg)
				apply[0]() // no-op snapshot verification; bounded progress queue
				command := apply[1]
				for i := 0; command != nil && i < 10; i++ {
					command = deliver(m, command)
				}
			}
			if m.wizard != nil || m.selected != overview || !m.opened {
				t.Fatal("wizard did not return to dashboard")
			}
		})
	}
	m, _ := testModel(t)
	m.showSystemSetup()
	old := m.wizard
	m.showSystemSetup()
	m.Update(setupwizard.FinishedMsg{Session: old})
	if m.wizard == nil {
		t.Fatal("previous session closed new wizard")
	}
	old.Close(nil)
}

func TestQuitAndContextCancellation(t *testing.T) {
	for _, s := range []section{overview, systemSetup, services, network, vpn, security, storage, packages, logs} {
		t.Run(sectionNames[s], func(t *testing.T) {
			m, _ := testModel(t)
			m.selected = s
			m.openSection()
			if !isQuit(press(m, "ctrl+c")) || m.ctx.Err() == nil {
				t.Fatal("Ctrl+C failed")
			}
		})
	}
	m, _ := testModel(t)
	m.Update(cancelledMsg{})
	if !m.quitting || m.ctx.Err() == nil {
		t.Fatal("parent cancellation ignored")
	}
}
