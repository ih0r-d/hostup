package setupwizard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/ih0r-d/hostup/internal/setup"
)

// Run requires both input and output to be terminals; it never opens /dev/tty
// to bypass redirection. System changes require the model's explicit review.
func Run(ctx context.Context, engine *setup.Engine, in io.Reader, out io.Writer) error {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	if !inputOK || !outputOK || !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		return fmt.Errorf("setup wizard requires interactive input and output terminals; use setup plan or setup apply --yes for automation")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	m := newModel(ctx, engine, os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb")
	// Cancellation reaches the model first. Killing Bubble Tea's context directly
	// would discard the engine's partial result while an operation is in flight.
	program := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out),
		tea.WithContext(context.WithoutCancel(ctx)), tea.WithoutSignalHandler())
	_, programErr := program.Run()
	m.stop(programErr)
	summary := m.withDetails(m.summary())
	if programErr != nil {
		// The terminal UI is unavailable, so its own failure cannot be inspected
		// with the Diagnostics key. Keep the underlying engine output opt-in.
		summary += "\nTerminal error: " + ansi.Strip(programErr.Error())
	}
	if _, err := fmt.Fprintln(out, summary); err != nil {
		return fmt.Errorf("write setup wizard summary: %w", errors.Join(m.err, err))
	}
	if m.err != nil {
		return runError{m.err}
	}
	return nil
}

// stop joins any started application before returning control to the caller.
func (m *model) stop(programErr error) {
	m.cancel()
	if task := m.application; task != nil && task.started.Load() {
		// A rendering/input failure must not leave state-changing work behind.
		<-task.done
		m.result = task.result
		if programErr != nil && m.err == nil {
			m.err = task.err
		}
	}
	if programErr != nil {
		m.err = errors.Join(m.err, programErr)
	}
}

// The full cause remains inspectable via errors.Is/As, but command output is
// shown only when the user explicitly opens Diagnostics in the wizard.
type runError struct{ cause error }

func (e runError) Error() string { return "setup wizard did not complete; see the summary above" }
func (e runError) Unwrap() error { return e.cause }
