package setupwizard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Keep the plain post-wizard summary independent of terminal styling.
func (m *model) withDetails(text string) string {
	if m.err != nil && m.details {
		return text + "\n\nDiagnostics:\n" + ansi.Strip(m.err.Error())
	}
	return text
}

func (m *model) summary() string {
	title := "Setup complete"
	if m.err != nil {
		title = "Setup stopped"
		if m.cancelling || errors.Is(m.err, context.Canceled) {
			title = "Setup cancelled"
		}
	} else if len(m.result.Applied) == 0 {
		title = "Already configured"
	}
	text := "Hostup — " + title + "\n"
	if m.application == nil {
		text += "No changes were applied.\n"
		if m.err != nil && !m.cancelling && m.current.Hostname == "" {
			text += "Could not read current setup; Linux with native systemd tools is required.\n"
		}
	}
	for _, group := range []struct {
		label  string
		fields []string
	}{{"Applied", m.result.Applied}, {"Unchanged", m.result.Unchanged}, {"Not attempted", m.result.Pending}} {
		for _, field := range group.fields {
			text += fmt.Sprintf("%s: %s\n", group.label, field)
		}
	}
	if m.result.Failed != "" {
		text += "Failed: " + m.result.Failed + " (may have changed).\nInspect current state with hostup setup plan.\n"
	}
	if m.err != nil {
		text += "\n" + friendlyError(m.err)
	}
	return strings.TrimSpace(text)
}

func friendlyError(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission):
		return "Insufficient permissions. Rerun with the required privileges explicitly."
	case errors.Is(err, context.Canceled):
		return "Cancellation requested. Completed operations above remain applied."
	case errors.Is(err, context.DeadlineExceeded):
		return "An operation timed out. Inspect the current state before trying again."
	default:
		return "Setup could not finish. Open Diagnostics for details."
	}
}
