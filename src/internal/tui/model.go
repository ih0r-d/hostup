package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	networkinfo "github.com/ih0r-d/hostup/src/internal/network"
	"github.com/ih0r-d/hostup/src/internal/setup"
	"github.com/ih0r-d/hostup/src/internal/system"
	setupwizard "github.com/ih0r-d/hostup/src/internal/tui/setup"
)

type section int

const (
	overview section = iota
	systemSetup
	services
	network
	vpn
	security
	storage
	packages
	logs
)

var sectionNames = [...]string{"Overview", "System Setup", "Services", "Network", "VPN", "Security", "Storage", "Packages", "Logs"}

type dependencies struct {
	diagnose func() (system.Diagnostics, error)
	services *system.ServiceManager
	setup    *setup.Engine
	network  func(context.Context) (networkinfo.Report, error)
}

type cancelledMsg struct{}

type model struct {
	ctx           context.Context
	cancel        context.CancelFunc
	deps          dependencies
	selected      section
	opened        bool
	width, height int
	scroll        int
	requestID     int
	requestCancel context.CancelFunc
	overview      overviewState
	services      servicesState
	network       networkState
	wizard        *setupwizard.Session
	quitting      bool
	noColor       bool
	profile       colorprofile.Profile
	logo          string
}

func newModel(ctx context.Context, deps dependencies, noColor bool) *model {
	ctx, cancel := context.WithCancel(ctx)
	return &model{ctx: ctx, cancel: cancel, deps: deps, width: 80, height: 24,
		noColor: noColor, profile: colorprofile.TrueColor}
}

func (m *model) Init() tea.Cmd {
	watch := func() tea.Msg { <-m.ctx.Done(); return cancelledMsg{} }
	return watch
}

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case cancelledMsg:
		return m, m.quit()
	case tea.WindowSizeMsg:
		m.width, m.height = max(0, msg.Width), max(0, msg.Height)
		m.prepareLogo()
	case tea.ColorProfileMsg:
		m.profile = msg.Profile
		m.logo = ""
		m.prepareLogo()
	case setupwizard.FinishedMsg:
		if msg.Session != m.wizard {
			return m, nil
		}
		m.wizard.Close(nil)
		m.wizard = nil
		if m.quitting {
			return m, tea.Quit
		}
		m.selected = overview
		return m, m.showOverview()
	case overviewMsg:
		m.receiveOverview(msg)
		return m, nil
	case servicesMsg:
		m.receiveServices(msg)
		return m, nil
	case networkMsg:
		m.receiveNetwork(msg)
		return m, nil
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		if m.wizard == nil {
			return m, m.key(msg.String())
		}
	}
	if m.wizard != nil {
		command := m.wizard.Update(message)
		if m.quitting && !m.wizard.Applying() {
			return m, tea.Quit
		}
		return m, command
	}
	return m, nil
}

func (m *model) key(key string) tea.Cmd {
	if !m.opened {
		switch key {
		case "up", "k":
			m.selected = max(overview, m.selected-1)
		case "down", "j":
			m.selected = min(logs, m.selected+1)
		case "enter":
			if m.width >= 24 && m.height >= 12 {
				return m.openSection()
			}
		case "q":
			return m.quit()
		}
		return nil // One navigation control: Tab has nothing to cycle.
	}
	if m.selected == services {
		return m.servicesKey(key)
	}
	if m.selected == network {
		return m.networkKey(key)
	}
	switch key {
	case "esc":
		m.leaveSection()
	case "up", "k":
		m.scroll = max(0, m.scroll-1)
	case "down", "j":
		m.scroll = min(m.scroll+1, m.maxScroll())
	}
	return nil
}

func (m *model) openSection() tea.Cmd {
	switch m.selected {
	case overview:
		return m.showOverview()
	case systemSetup:
		return m.showSystemSetup()
	case services:
		return m.showServices()
	case network:
		return m.showNetwork()
	case vpn:
		return m.showVPN()
	case security:
		return m.showSecurity()
	case storage:
		return m.showStorage()
	case packages:
		return m.showPackages()
	case logs:
		return m.showLogs()
	}
	return nil
}

func (m *model) enter(s section) {
	m.stopRequest()
	m.selected, m.opened, m.scroll = s, true, 0
}

func (m *model) leaveSection() {
	m.stopRequest()
	m.opened, m.scroll = false, 0
}

func (m *model) stopRequest() {
	if m.requestCancel != nil {
		m.requestCancel()
		m.requestCancel = nil
	}
	m.requestID++ // Responses from a screen the user left are ignored.
}

func (m *model) request() (context.Context, context.CancelFunc, int) {
	m.stopRequest()
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	m.requestCancel = cancel
	return ctx, cancel, m.requestID
}

func (m *model) showSystemSetup() tea.Cmd {
	m.enter(systemSetup)
	m.wizard = setupwizard.NewSession(m.ctx, m.deps.setup, m.monochrome())
	m.wizard.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	m.wizard.Update(tea.ColorProfileMsg{Profile: m.profile})
	return m.wizard.Init()
}

func (m *model) quit() tea.Cmd {
	if m.quitting {
		return nil
	}
	m.quitting = true
	m.cancel()
	m.stopRequest()
	if m.wizard != nil {
		command := m.wizard.Update(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
		if m.wizard.Applying() {
			return command // Drain progress/results before leaving the event loop.
		}
	}
	return tea.Quit
}

func (m *model) close(err error) {
	m.cancel()
	m.stopRequest()
	if m.wizard != nil {
		m.wizard.Close(err)
	}
}
