package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/ih0r-d/hostup/src/internal/tui/palette"
)

type overviewState struct {
	loading bool
	report  system.Diagnostics
	err     error
}

type overviewMsg struct {
	id     int
	report system.Diagnostics
	err    error
}

func (m *model) showOverview() tea.Cmd {
	m.enter(overview)
	m.overview = overviewState{loading: true}
	ctx, cancel, id := m.request()
	diagnose := m.deps.diagnose
	return func() tea.Msg {
		defer cancel()
		if err := ctx.Err(); err != nil {
			return overviewMsg{id: id, err: err}
		}
		// The existing adapter reads local files/statfs. Run it off the event loop;
		// check cancellation before/after without inventing a monitoring backend.
		report, err := diagnose()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return overviewMsg{id, report, err}
	}
}

func (m *model) receiveOverview(msg overviewMsg) {
	if m.opened && m.selected == overview && msg.id == m.requestID {
		m.overview = overviewState{report: msg.report, err: msg.err}
	}
}

func (m *model) overviewBody(t theme, width int) string {
	if m.overview.loading {
		return t.muted.Render("Reading system information…")
	}
	if err := m.overview.err; err != nil {
		return t.warning.Render("Diagnostics unavailable") + "\n\n" +
			readError(err, "Could not collect diagnostics. Use hostup doctor for details.")
	}
	d := m.overview.report
	host := t.muted.Render("HOST") + "\n" + t.title.Render(safe(d.Platform.Hostname)) + "\n" +
		safe(d.Platform.Distribution+" "+d.Platform.Version) + "  ·  " + safe(d.Platform.Architecture)
	metrics := []struct{ label, value, detail string }{
		{"CPU", fmt.Sprintf("%d", d.CPU.LogicalCPUs), "logical CPUs"},
		{"MEMORY", gib(d.Memory.AvailableBytes) + " available", gib(d.Memory.TotalBytes) + " total"},
		{"DISK " + safe(d.Disk.Path), gib(d.Disk.UsedBytes) + " used", gib(d.Disk.TotalBytes) + " total"},
	}
	var cards []string
	for _, metric := range metrics {
		cards = append(cards, t.muted.Render(metric.label)+"\n"+t.title.Render(metric.value)+"\n"+t.muted.Render(metric.detail))
	}
	var resources string
	if width >= 68 {
		cardWidth := (width - 4) / 3
		for i := range cards {
			cards[i] = palette.Surface(t.panel.Width(cardWidth).Render(fit(cards[i], cardWidth-4, 4)), m.profile, palette.Panel, m.monochrome())
		}
		resources = lipgloss.JoinHorizontal(lipgloss.Top, cards[0], "  ", cards[1], "  ", cards[2])
	} else {
		resources = strings.Join(cards, "\n\n")
	}
	return palette.Surface(t.panel.Width(width).Render(fit(host, width-4, 4)), m.profile, palette.Panel, m.monochrome()) + "\n\n" + resources +
		"\n\n" + t.success.Render("Diagnostics collected") + "\n" + t.muted.Render("Snapshot · no live monitoring")
}

func gib(bytes uint64) string { return fmt.Sprintf("%.2f GiB", float64(bytes)/(1<<30)) }

func readError(err error, fallback string) string {
	switch {
	case strings.Contains(strings.ToLower(err.Error()), "unsupported"):
		return "Unavailable on this platform. Linux is required."
	case errors.Is(err, context.Canceled):
		return "Request cancelled."
	case errors.Is(err, context.DeadlineExceeded):
		return "Request timed out."
	default:
		return fallback
	}
}
