package setupwizard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/setup"
)

// View formats stored UI state. Planning, validation and execution stay in the
// model and setup engine; theme and layout are separate presentation concerns.
func (m *model) View() tea.View {
	var content string
	if m.width < minimumWidth || m.height < minimumHeight {
		message := "HOSTUP\nResize to 32 × 14\nCtrl+C Cancel"
		if m.page == applying {
			message = "HOSTUP · Working\nCtrl+C Cancel safely"
		}
		lines := strings.Split(message, "\n")
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], max(0, m.width), "")
		}
		content = strings.Join(lines[:min(len(lines), max(0, m.height))], "\n")
	} else {
		content = m.layout(m.body(), newTheme(m.noColor, m.dark))
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m *model) body() string {
	t := newTheme(m.noColor, m.dark)
	switch m.page {
	case welcome:
		os := t.muted.Render("OS  " + strings.ReplaceAll(m.platform, "/", " · "))
		if !m.loaded {
			return t.title.Render("Quick setup") + "\n\n" + os + "\nReading settings…"
		}
		if m.err != nil {
			caption := friendlyError(m.err)
			if !strings.HasPrefix(m.platform, "linux/") {
				caption = "Linux with systemd is required."
			}
			return m.withDetails(t.warning.Render("Setup unavailable") + "\n\n" + os +
				"\n" + caption)
		}
		return t.title.Render("Make this host yours.") + "\n" +
			t.muted.Render("Set its hostname and timezone.") + "\n\n" + os
	case configure:
		return m.fieldView(0) + "\n\n" + m.fieldView(1) + m.note(t)
	case review:
		return m.reviewView(t)
	case applying:
		title := "Applying changes"
		if !m.plan.HasChanges() {
			title = "Checking settings"
		}
		if m.cancelling {
			title = "Cancelling…"
		}
		lines := []string{t.title.Render(title), ""}
		for i, name := range []string{"Hostname", "Timezone"} {
			label, style := "Queued", t.muted
			switch setup.OperationState(m.operationStates[i]) {
			case setup.Running:
				label, style = "Running…", t.accent
			case setup.Applied:
				label, style = "+ Applied", t.success
			case setup.Unchanged:
				label = "= Unchanged"
			case setup.Failed:
				label, style = "! Failed", t.warning
			}
			lines = append(lines, aligned(name, style.Render(label), m.contentWidth()))
		}
		if m.cancelling {
			lines = append(lines, "", t.muted.Render("Waiting for the active operation."))
		}
		return strings.Join(lines, "\n")
	case results:
		return m.withDetails(m.resultView(t))
	}
	return ""
}

func (m *model) fieldView(index int) string {
	t := newTheme(m.noColor, m.dark)
	item := []struct{ name, current, invalid string }{
		{"Hostname", m.current.Hostname, "invalid hostname · lowercase DNS, max 64 bytes"},
		{"Timezone", m.current.Timezone, "invalid timezone · use an IANA location, e.g. UTC"},
	}[index]
	label, marker := t.title.Render(item.name), "  "
	if m.focus == index {
		label, marker = t.accent.Render(item.name), t.accent.Render("> ")
	}
	current := "  " + t.muted.Render(ansi.Truncate("current: "+item.current, max(1, m.contentWidth()-2), "…"))
	field := label + "\n" + marker + m.inputs[index].View() + "\n" + current
	if m.inputs[index].Err != nil {
		field += "\n" + t.warning.Render("! "+item.invalid)
	}
	return field
}

func (m *model) reviewView(t theme) string {
	title := t.title.Render("Review changes")
	if !m.plan.HasChanges() {
		title = t.success.Render("Already configured")
	}
	lines := []string{title, ""}
	for _, item := range []struct {
		name   string
		change setup.Change
	}{
		{"Hostname", m.plan.Hostname}, {"Timezone", m.plan.Timezone},
	} {
		badge := t.muted.Render("= SAME")
		value := item.change.Current + " → " + item.change.Desired
		if item.change.Changed {
			badge = t.accent.Render("~ CHANGE")
			value = t.muted.Render(item.change.Current) + t.accent.Render(" → ") + t.success.Render(item.change.Desired)
		}
		rowWidth := m.contentWidth() - t.row.GetHorizontalFrameSize()
		row := aligned(t.muted.Render(strings.ToUpper(item.name)), badge, rowWidth) + "\n" + value
		lines = append(lines, t.row.Width(m.contentWidth()).Render(row), "")
	}
	if !m.plan.HasChanges() {
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	count := 0
	if m.plan.Hostname.Changed {
		count++
	}
	if m.plan.Timezone.Changed {
		count++
	}
	noun := "changes"
	if count == 1 {
		noun = "change"
	}
	check := fmt.Sprintf("[ ] Confirm %d %s", count, noun)
	style := t.row
	if m.confirmed {
		check = fmt.Sprintf("[x] Ready to apply %d %s", count, noun)
		style = t.ready
	}
	return strings.Join(lines, "\n") + style.Width(m.contentWidth()).Render(check) + m.note(t)
}

func (m *model) note(t theme) string {
	if m.notice == "" {
		return ""
	}
	return "\n" + t.muted.Render(m.notice)
}

func (m *model) resultView(t theme) string {
	title, style := "Setup complete", t.success
	if m.err != nil {
		title, style = "Setup stopped", t.warning
		if len(m.result.Applied) > 0 {
			title = "Partially applied"
		}
		if m.cancelling || errors.Is(m.err, context.Canceled) {
			title = "Cancelled"
		}
	} else if len(m.result.Applied) == 0 {
		title = "Already configured"
	}
	lines := []string{style.Render(title), ""}
	if m.application == nil {
		lines = append(lines, t.muted.Render("No changes applied."))
	}
	for _, setting := range []struct{ key, label string }{{"hostname", "Hostname"}, {"timezone", "Timezone"}} {
		var state string
		stateStyle := t.muted
		switch {
		case m.result.Failed == setting.key:
			state, stateStyle = "! Failed", t.warning
		case slices.Contains(m.result.Applied, setting.key):
			state, stateStyle = "+ Applied", t.success
		case slices.Contains(m.result.Unchanged, setting.key):
			state = "= Unchanged"
		case slices.Contains(m.result.Pending, setting.key):
			state = "– Not attempted"
		default:
			continue
		}
		lines = append(lines, aligned(setting.label, stateStyle.Render(state), m.contentWidth()))
	}
	if m.result.Failed != "" {
		lines = append(lines, "", "Failed setting may have changed.", t.muted.Render("Inspect with hostup setup plan."))
	}
	if m.err != nil {
		if hint := m.resultHint(); hint != "" {
			lines = append(lines, "", hint)
		}
	} else if len(m.result.Applied) > 0 {
		noun := "settings"
		if len(m.result.Applied) == 1 {
			noun = "setting"
		}
		lines = append(lines, "", t.muted.Render(fmt.Sprintf("%d %s updated.", len(m.result.Applied), noun)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) resultHint() string {
	switch {
	case errors.Is(m.err, os.ErrPermission):
		return "Insufficient permissions. Rerun with the required privileges."
	case errors.Is(m.err, context.DeadlineExceeded):
		return "Operation timed out. Check current settings."
	case len(m.result.Applied) > 0 && errors.Is(m.err, context.Canceled):
		return "Completed changes remain applied."
	default:
		return ""
	}
}

func (m *model) controls(scrollable bool) string {
	var hints string
	switch m.page {
	case welcome:
		hints = "enter continue · ^c cancel"
		if !m.loaded {
			hints = "^c cancel"
		}
		if m.err != nil {
			hints = "enter close · d details"
		}
	case configure:
		hints = "tab field · enter review · esc back · ^c cancel"
	case review:
		hints = "space confirm · esc back · ^c cancel"
		if m.confirmed {
			hints = "enter apply · space undo · esc back · ^c cancel"
		}
		if !m.plan.HasChanges() {
			hints = "enter done · esc back"
		}
	case applying:
		hints = "^c cancel"
	case results:
		hints = "enter done"
		if m.err != nil {
			hints += " · d details"
		}
	}
	if scrollable {
		hints += " · pgup/pgdn scroll"
	}
	return hints
}
