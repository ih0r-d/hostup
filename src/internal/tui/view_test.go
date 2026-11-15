package tui

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestLayoutsWithinViewport(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
		for _, page := range []string{"navigation", "overview", "services", "details", "VPN", "wizard"} {
			t.Run(fmt.Sprintf("%s/%s", profile, page), func(t *testing.T) {
				m, _ := testModel(t)
				m.noColor = false
				m.Update(tea.ColorProfileMsg{Profile: profile})
				switch page {
				case "overview":
					deliver(m, m.showOverview())
				case "services":
					deliver(m, m.showServices())
				case "details":
					deliver(m, m.showServices())
					deliver(m, press(m, "enter"))
				case "VPN":
					m.showVPN()
				case "wizard":
					m.showSystemSetup()
				}
				for _, size := range [][2]int{{140, 40}, {110, 28}, {96, 22}, {80, 24}, {44, 18}, {32, 14}, {24, 12}, {24, 8}, {12, 4}, {1, 1}, {0, 0}} {
					m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
					view := m.View()
					if !view.AltScreen {
						t.Fatal("missing alternate screen")
					}
					if size[1] > 0 && len(strings.Split(view.Content, "\n")) > size[1] {
						t.Fatalf("height overflow %v: %q", size, view.Content)
					}
					for _, line := range strings.Split(view.Content, "\n") {
						if ansi.StringWidth(line) > size[0] {
							t.Fatalf("width overflow %v: %q", size, line)
						}
					}
					if profile == colorprofile.ASCII && strings.Contains(view.Content, "\x1b[") {
						t.Fatal("monochrome contains ANSI")
					}
				}
			})
		}
	}
}

func TestBrandThemeFocusAndPreviews(t *testing.T) {
	m, _ := testModel(t)
	m.noColor = false
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if m.logo == "" || !strings.Contains(m.logo, "▀") {
		t.Fatal("original PNG rendering not reused")
	}
	dark := m.View().Content
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if m.View().Content != dark {
		t.Fatal("light background changed Hostup's dark identity")
	}
	m.noColor = true
	plain := m.View().Content
	if strings.Contains(plain, "\x1b[") || strings.Contains(plain, "▀") {
		t.Fatal("NO_COLOR did not use simple branding")
	}
	for _, label := range sectionNames {
		if !strings.Contains(plain, label) {
			t.Fatalf("missing section %s", label)
		}
	}
	if !strings.Contains(plain, "> 01 Overview") || !strings.Contains(plain, "NAVIGATION") {
		t.Fatal("navigation focus not visible")
	}
	deliver(m, m.showOverview())
	if strings.Contains(m.View().Content, "NAVIGATION") {
		t.Fatal("focus did not move into screen")
	}
	t.Log("wide Overview:\n" + ansi.Strip(m.View().Content))
	m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	if strings.Contains(m.View().Content, "LINUX MANAGEMENT") {
		t.Fatal("sidebar not collapsed")
	}
	m.showVPN()
	for i := 0; i < 10; i++ {
		press(m, "j")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "OpenVPN") {
		t.Fatal("narrow placeholder cannot scroll to all providers")
	}
	t.Log("narrow VPN:\n" + ansi.Strip(m.View().Content))
}

func TestUntrustedDisplayValues(t *testing.T) {
	m, _ := testModel(t)
	deliver(m, m.showOverview())
	m.overview.report.Platform.Hostname = "server\x1b[2J\nFAKE\t\a"
	body := m.body(m.theme(), 70, 24)
	if strings.ContainsAny(body, "\a\t") || strings.Contains(body, "\x1b[2J") || strings.Contains(body, "server\n") {
		t.Fatal("host name escaped its display field")
	}
}

func TestNonInteractiveGuard(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if IsInteractive(strings.NewReader(""), io.Discard) {
		t.Fatal("nonterminal supported")
	}
	if err := Run(context.Background(), strings.NewReader(""), io.Discard); err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Fatal(err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	if IsInteractive(read, write) {
		t.Fatal("pipes accepted")
	}
	t.Setenv("TERM", "dumb")
	if IsInteractive(os.Stdin, os.Stdout) {
		t.Fatal("dumb terminal accepted")
	}
}

func TestOneHeadlessApplicationLoop(t *testing.T) {
	m, f := testModel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := make(chan string, 4)
	p := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler(), tea.WithWindowSize(80, 24), tea.WithContext(ctx), tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
		switch msg := msg.(type) {
		case overviewMsg:
			observed <- "overview"
		case servicesMsg:
			if msg.details {
				observed <- "details"
			} else {
				observed <- "list"
			}
		}
		return msg
	}))
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	send := func(code rune) { p.Send(tea.KeyPressMsg(tea.Key{Code: code})) }
	await := func(want string) {
		t.Helper()
		select {
		case got := <-observed:
			if got != want {
				t.Fatalf("got %s want %s", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	send(tea.KeyEnter)
	await("overview")
	send(tea.KeyEscape)
	send(tea.KeyDown)
	send(tea.KeyDown)
	send(tea.KeyEnter)
	await("list")
	send(tea.KeyEnter)
	await("details")
	send(tea.KeyEscape)
	send(tea.KeyEscape)
	send('q')
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("program did not exit")
	}
	if len(f.calls) != 2 {
		t.Fatal(f.calls)
	}
}
