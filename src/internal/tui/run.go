// Package tui composes Hostup's navigation and screens in one Bubble Tea loop.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	networkinfo "github.com/ih0r-d/hostup/src/internal/network"
	"github.com/ih0r-d/hostup/src/internal/setup"
	"github.com/ih0r-d/hostup/src/internal/system"
)

// IsInteractive respects redirected streams and unsupported dumb terminals.
// It never opens /dev/tty to bypass the caller's input/output choices.
func IsInteractive(in io.Reader, out io.Writer) bool {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	return inputOK && outputOK && os.Getenv("TERM") != "dumb" &&
		term.IsTerminal(input.Fd()) && term.IsTerminal(output.Fd())
}

func Run(ctx context.Context, in io.Reader, out io.Writer) error {
	if !IsInteractive(in, out) {
		return fmt.Errorf("hostup tui requires interactive input and output terminals; TERM=dumb is unsupported")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	m := newModel(ctx, dependencies{diagnose: system.Diagnose, services: system.NewServiceManager(nil), setup: setup.NewEngine(nil, nil), network: networkinfo.Inspect}, os.Getenv("NO_COLOR") != "")
	// Let the model drain an in-flight setup operation before exiting. On terminal
	// failure, close still cancels/joins it after Bubble Tea restores the terminal.
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out),
		tea.WithContext(context.WithoutCancel(ctx)), tea.WithoutSignalHandler())
	_, err := p.Run()
	m.close(err)
	if m.wizard != nil && (m.quitting || err != nil) {
		// Alternate-screen contents disappear on exit. Keep completed/failed
		// settings visible after a global cancellation or terminal failure.
		_, writeErr := fmt.Fprintln(out, m.wizard.Summary())
		err = errors.Join(err, writeErr)
	}
	if err != nil {
		return fmt.Errorf("Hostup terminal interface: %w", err)
	}
	return nil
}
