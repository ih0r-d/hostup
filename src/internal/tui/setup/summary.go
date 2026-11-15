package setupwizard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/setup"
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
		} else if errors.Is(m.err, setup.ErrVerificationMismatch) {
			title = "Verification failed"
		} else if errors.Is(m.err, setup.ErrVerificationUnavailable) {
			title = "Verification unavailable"
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
	}{{"Applied", m.result.Applied}, {"Executed (not verified)", m.result.Unverified()}, {"Unchanged", m.result.Unchanged}, {"Not attempted", m.result.Pending}} {
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
	case errors.Is(err, setup.ErrVerificationMismatch):
		var failure *setup.VerificationError
		if errors.As(err, &failure) {
			return "Verification failed: " + failure.Setting + " does not match the requested value. Inspect with hostup setup plan."
		}
		return "Verification failed. Inspect with hostup setup plan."
	case errors.Is(err, setup.ErrVerificationUnavailable):
		return "Verification unavailable. Resulting state is unknown; inspect with hostup setup plan."
	case errors.Is(err, context.Canceled):
		return "Cancellation requested. Inspect current state; completed changes were not rolled back."
	case errors.Is(err, context.DeadlineExceeded):
		return "An operation timed out. Inspect the current state before trying again."
	case errors.Is(err, setup.ErrStalePlan):
		return "Setup changed since review. Inspect and review a new plan."
	default:
		return "Setup could not finish. Open Diagnostics for details."
	}
}
