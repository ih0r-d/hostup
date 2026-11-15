package setupwizard

import (
	"context"
	"errors"
	"image/color"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/internal/setup"
)

func TestScreenLayout(t *testing.T) {
	for _, tt := range []struct {
		name string
		page step
		set  func(*model)
		want []string
	}{
		{"welcome", welcome, nil, []string{"Make this host yours.", "OS  ", "[ enter Continue ]"}},
		{"configuration", configure, nil, []string{"Hostname", "> new-host", "current: old-host", "[ enter Review ]"}},
		{"review", review, nil, []string{"~ CHANGE", "= SAME", "old-host → new-host", "[ ] Confirm 1 change"}},
		{"no-change", review, func(m *model) {
			m.plan = setup.Plan{Hostname: setup.Change{Current: "old-host", Desired: "old-host"}, Timezone: setup.Change{Current: "UTC", Desired: "UTC"}}
		}, []string{"Already configured", "[ enter Done ]"}},
		{"validation", configure, func(m *model) { edit(m, 0, "bad name") }, []string{"! invalid hostname", "max 64 bytes"}},
		{"applying", applying, func(m *model) { m.operationStates = [2]string{"applied", "running"} }, []string{"Applying changes", "+ Applied", "Running…"}},
		{"success", results, func(m *model) {
			m.application = &application{}
			m.result = setup.Result{Applied: []string{"hostname"}, Unchanged: []string{"timezone"}}
		}, []string{"Setup complete", "+ Applied", "= Unchanged", "1 setting updated."}},
		{"partial-failure", results, func(m *model) {
			m.application = &application{}
			m.err = errors.New("private diagnostic")
			m.result = setup.Result{Applied: []string{"hostname"}, Failed: "timezone"}
		}, []string{"Partially applied", "+ Applied", "! Failed", "may have changed"}},
		{"permission", results, func(m *model) {
			m.application = &application{}
			m.err = os.ErrPermission
			m.result = setup.Result{Failed: "hostname", Pending: []string{"timezone"}}
		}, []string{"Setup stopped", "Insufficient permissions", "Not attempted"}},
		{"cancelled", results, func(m *model) { m.err, m.cancelling = context.Canceled, true }, []string{"Cancelled", "No changes applied."}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
			m := readyModel(t, f)
			m.platform = "linux/arm64"
			press(m, tea.KeyEnter)
			edit(m, 0, "new-host")
			reviewPlan(t, m)
			m.page = tt.page
			if tt.set != nil {
				tt.set(m)
			}
			plain := ansi.Strip(m.View().Content)
			for _, want := range tt.want {
				if !strings.Contains(plain, want) {
					t.Fatalf("screen missing %q:\n%s", want, plain)
				}
			}
			if strings.Contains(plain, "private diagnostic") {
				t.Fatal("diagnostics were exposed by default")
			}
			if strings.Contains(m.controls(false), "Scroll") || strings.Contains(m.controls(false), "pgup") {
				t.Fatal("scroll controls shown without overflow")
			}
			lines := strings.Split(plain, "\n")
			if len(lines) != 24 || strings.TrimSpace(lines[0]) != "" {
				t.Fatal("compact interface is not vertically centered")
			}
			for _, line := range lines {
				if strings.TrimSpace(line) != "" && strings.IndexFunc(line, func(r rune) bool { return r != ' ' }) < 6 {
					t.Fatal("interface does not honor its centered maximum width")
				}
			}
			// Verbose test output doubles as reproducible, rootless screen previews.
			t.Log("\n" + plain)
			for _, size := range [][2]int{{120, 40}, {80, 28}, {64, 32}, {50, 24}, {50, 18}, {32, 24}, {32, 14}} {
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				view := m.View().Content
				if len(strings.Split(view, "\n")) > size[1] {
					t.Fatalf("%s exceeds height at %v", tt.name, size)
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("%s exceeds width at %v: %q", tt.name, size, line)
					}
				}
			}
		})
	}
}

func TestThemeAndFocus(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	m.Update(tea.WindowSizeMsg{Width: 32, Height: 14})
	press(m, tea.KeyTab)
	if plain := ansi.Strip(m.View().Content); !strings.Contains(plain, "Timezone") || !strings.Contains(plain, "> UTC") {
		t.Fatalf("focused field is hidden in narrow layout:\n%s", plain)
	}
	m.noColor = false
	dark := m.View().Content
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if m.dark || m.View().Content == dark {
		t.Fatal("theme did not adapt to a light terminal")
	}
	m.noColor = true
	light := m.View().Content
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if !m.dark || m.View().Content != light {
		t.Fatal("monochrome presentation depends on terminal colors")
	}
}

func TestNoOpSnapshotStillChecked(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	reviewPlan(t, m)
	press(m, tea.KeySpace)
	if m.confirmed {
		t.Fatal("no-op review exposed a confirmation state")
	}
	f.current.Hostname = "external-host"
	finishApplication(t, m, press(m, tea.KeyEnter))
	if m.err == nil || len(f.calls) != 0 {
		t.Fatal("Done on a stale no-op plan wrote configuration or claimed success")
	}
}

func TestMockupLayoutAndOriginalLogo(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	edit(m, 0, "new-host")
	reviewPlan(t, m)
	m.noColor = false
	m.platform = "linux/arm64"
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.logo == "" || !strings.Contains(m.logo, "▀") {
		t.Fatal("wide layout did not render the embedded PNG logo")
	}
	lines := strings.Split(m.logo, "\n")
	if len(lines) != 3 {
		t.Fatal("logo exceeds its compact terminal footprint")
	}
	for _, line := range lines {
		if ansi.StringWidth(line) != 9 {
			t.Fatal("logo has inconsistent cell width")
		}
	}
	plain := ansi.Strip(m.View().Content)
	for _, want := range []string{"LINUX MANAGEMENT", "02 System Setup", "LOCAL HOST", "System Setup", "Hostname and timezone"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("wide mockup layout missing %q:\n%s", want, plain)
		}
	}
	for _, placeholder := range []string{"Overview", "Network", "VPN", "Services", "Storage", "Logs"} {
		if strings.Contains(plain, placeholder) {
			t.Fatalf("unimplemented navigation entry displayed: %s", placeholder)
		}
	}
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.ASCII})
	if !m.noColor || m.logo != "" || strings.Contains(m.View().Content, "▀") {
		t.Fatal("colorless terminal retained the raster logo")
	}
	if !strings.Contains(m.View().Content, "┏") {
		t.Fatal("monochrome progress has no structural active-step indicator")
	}
	t.Log("\n" + ansi.Strip(m.View().Content))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	plain = ansi.Strip(m.View().Content)
	if strings.Contains(plain, "LINUX MANAGEMENT") || !strings.Contains(plain, ">_ HOSTUP") || len(f.calls) != 0 {
		t.Fatal("compact layout did not collapse navigation safely")
	}
}
