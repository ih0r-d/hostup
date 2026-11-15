package setupwizard

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/setup"
)

type systemFixture struct {
	current setup.Configuration
	reads   int
	calls   []string
	fail    string
	err     error
	block   string
	entered chan struct{}
}

func (f *systemFixture) engine() *setup.Engine {
	return setup.NewEngine(func(context.Context) (setup.Configuration, error) {
		f.reads++
		return f.current, nil
	}, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, name)
		if name == f.block {
			close(f.entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if name == f.fail {
			return nil, f.err
		}
		if name == "hostnamectl" {
			f.current.Hostname = args[2]
		} else {
			f.current.Timezone = args[2]
		}
		return nil, nil
	})
}

func readyModel(t *testing.T, f *systemFixture) *model {
	t.Helper()
	m := newModel(context.Background(), f.engine(), true)
	t.Cleanup(m.cancel)
	// Execute only the read command, not the long-lived cancellation watcher.
	initial := m.Init()().(tea.BatchMsg)
	m.Update(initial[0]())
	return m
}

func press(m *model, code rune, modifiers ...tea.KeyMod) tea.Cmd {
	key := tea.Key{Code: code}
	for _, modifier := range modifiers {
		key.Mod |= modifier
	}
	if code >= ' ' && code <= '~' && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		key.Text = string(code)
	}
	_, cmd := m.Update(tea.KeyPressMsg(key))
	return cmd
}

func edit(m *model, index int, value string) {
	m.inputs[index].SetValue(value)
	m.validateFields()
}

func reviewPlan(t *testing.T, m *model) {
	t.Helper()
	cmd := press(m, tea.KeyEnter)
	if cmd == nil || !m.planning {
		t.Fatalf("review command missing: page=%v errors=%v,%v", m.page, m.inputs[0].Err, m.inputs[1].Err)
	}
	m.Update(cmd())
	if m.page != review || m.confirmed {
		t.Fatalf("review state=%+v", m)
	}
}

func finishApplication(t *testing.T, m *model, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("apply command missing")
	}
	commands := command().(tea.BatchMsg)
	go commands[0]()
	next := commands[1]
	for m.page == applying {
		if next == nil {
			t.Fatal("missing progress reader")
		}
		_, next = m.Update(next())
	}
	<-m.application.done
}

func TestNavigationAndConfirmation(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	if !strings.Contains(m.View().Content, "OS  ") || !strings.Contains(m.View().Content, "HOSTUP") {
		t.Fatal("welcome missing branding/environment")
	}
	press(m, tea.KeyEnter)
	if m.page != configure || !m.inputs[0].Focused() {
		t.Fatal("welcome did not continue to configuration")
	}
	press(m, tea.KeyTab)
	if m.focus != 1 {
		t.Fatal("Tab did not move focus")
	}
	press(m, tea.KeyTab, tea.ModShift)
	if m.focus != 0 {
		t.Fatal("Shift+Tab did not move focus")
	}
	edit(m, 0, "new-host")
	press(m, tea.KeyEscape)
	if m.page != welcome || m.inputs[0].Value() != "new-host" {
		t.Fatal("back lost the edit")
	}
	press(m, tea.KeyEnter)
	reviewPlan(t, m)
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "~ CHANGE") || !strings.Contains(view, "= SAME") || !strings.Contains(view, "old-host → new-host") {
		t.Fatalf("review=%q", view)
	}
	if command := press(m, tea.KeyEnter); command != nil || m.page != review || len(f.calls) != 0 {
		t.Fatal("Enter applied without explicit confirmation")
	}
	press(m, tea.KeySpace)
	if !m.confirmed {
		t.Fatal("Space did not confirm")
	}
	press(m, tea.KeyEscape)
	if m.page != configure || m.confirmed {
		t.Fatal("back did not reset confirmation")
	}
	reviewPlan(t, m)
	if len(f.calls) != 0 {
		t.Fatal("system changed before confirmation")
	}
	press(m, tea.KeySpace)
	command := press(m, tea.KeyEnter)
	if m.page != applying || len(f.calls) != 0 || strings.Contains(m.View().Content, "Setup complete") {
		t.Fatal("applied or claimed success before command completion")
	}
	finishApplication(t, m, command)
	if m.err != nil || !reflect.DeepEqual(f.calls, []string{"hostnamectl"}) || !strings.Contains(m.summary(), "Applied: hostname") {
		t.Fatalf("result=%+v error=%v calls=%v", m.result, m.err, f.calls)
	}
}

func TestInlineValidation(t *testing.T) {
	for _, tt := range []struct {
		name, value string
		field       int
	}{
		{"empty hostname", "", 0}, {"invalid hostname", "bad name", 0},
		{"empty timezone", "", 1}, {"invalid timezone", "Unknown/Zone", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
			m := readyModel(t, f)
			press(m, tea.KeyEnter)
			edit(m, tt.field, tt.value)
			if command := press(m, tea.KeyEnter); command != nil || m.page != configure || m.inputs[tt.field].Err == nil || len(f.calls) != 0 || f.reads != 1 {
				t.Fatal("invalid value reached planning/application")
			}
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 45})
			if !strings.Contains(m.View().Content, "invalid") {
				t.Fatalf("inline feedback missing: %q", m.View().Content)
			}
		})
	}
}

func TestUnchangedAndExistingValues(t *testing.T) {
	// Existing values are preserved even if they do not meet new-hostname rules.
	for _, hostname := range []string{"old-host", "ExistingHost"} {
		f := &systemFixture{current: setup.Configuration{Hostname: hostname, Timezone: "UTC"}}
		m := readyModel(t, f)
		press(m, tea.KeyEnter)
		reviewPlan(t, m)
		if m.plan.HasChanges() {
			t.Fatal("unchanged inputs created changes")
		}
		if strings.Contains(m.View().Content, "[ ] Confirm") || strings.Contains(m.View().Content, "enter Apply") {
			t.Fatal("no-op review asked for unnecessary confirmation")
		}
		finishApplication(t, m, press(m, tea.KeyEnter))
		if m.err != nil || len(f.calls) != 0 || len(m.result.Unchanged) != 2 || !strings.Contains(m.summary(), "Already configured") {
			t.Fatalf("unchanged result=%+v error=%v", m.result, m.err)
		}
		if m.ctx.Err() == nil {
			t.Fatal("Done did not close the no-op flow")
		}
	}
}

func TestApplyFailures(t *testing.T) {
	for _, tt := range []struct {
		name, fail string
		err        error
		applied    int
		want       string
	}{
		{"first operation", "hostnamectl", errors.New("raw command output"), 0, "Not attempted: timezone"},
		{"partial", "timedatectl", errors.New("raw command output"), 1, "Applied: hostname"},
		{"permission", "hostnamectl", os.ErrPermission, 0, "Insufficient permissions"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}, fail: tt.fail, err: tt.err}
			m := readyModel(t, f)
			press(m, tea.KeyEnter)
			edit(m, 0, "new-host")
			edit(m, 1, "Europe/Kyiv")
			reviewPlan(t, m)
			press(m, tea.KeySpace)
			finishApplication(t, m, press(m, tea.KeyEnter))
			if !errors.Is(m.err, tt.err) || len(m.result.Applied) != tt.applied || !strings.Contains(m.summary(), tt.want) || !strings.Contains(m.summary(), "may have changed") {
				t.Fatalf("result=%+v error=%v summary=%q", m.result, m.err, m.summary())
			}
			if strings.Contains(m.View().Content, "raw command output") {
				t.Fatal("raw command output shown without requesting diagnostics")
			}
			press(m, 'd')
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
			if !strings.Contains(m.View().Content, "Diagnostics:") {
				t.Fatal("diagnostics toggle failed")
			}
			calls := len(f.calls)
			press(m, tea.KeyEnter)
			if len(f.calls) != calls {
				t.Fatal("results silently retried an operation")
			}
		})
	}
}

func TestCancellationBeforeApply(t *testing.T) {
	for _, page := range []step{welcome, configure, review} {
		f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
		m := readyModel(t, f)
		m.page = page
		command := press(m, 'c', tea.ModCtrl)
		if command == nil || !errors.Is(m.err, context.Canceled) || len(f.calls) != 0 || !strings.Contains(m.summary(), "No changes were applied") {
			t.Fatalf("page=%v cancellation err=%v calls=%v", page, m.err, f.calls)
		}
	}
}

func TestCancellationDuringApply(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}, block: "timedatectl", entered: make(chan struct{})}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	edit(m, 0, "new-host")
	edit(m, 1, "Europe/Kyiv")
	reviewPlan(t, m)
	press(m, tea.KeySpace)
	commands := press(m, tea.KeyEnter)().(tea.BatchMsg)
	go commands[0]()
	<-f.entered
	if command := press(m, 'c', tea.ModCtrl); command != nil || m.page != applying || !m.cancelling {
		t.Fatal("cancel discarded an active operation's result")
	}
	next := commands[1]
	for m.page == applying {
		_, next = m.Update(next())
	}
	<-m.application.done
	if !errors.Is(m.err, context.Canceled) || !reflect.DeepEqual(m.result.Applied, []string{"hostname"}) || m.result.Failed != "timezone" || strings.Contains(m.summary(), "Setup complete") {
		t.Fatalf("cancelled result=%+v error=%v", m.result, m.err)
	}
}

func TestResizeAndMonochrome(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	edit(m, 0, "new-host")
	reviewPlan(t, m)
	press(m, tea.KeySpace)
	for _, size := range [][2]int{{100, 40}, {50, 18}, {32, 14}, {15, 5}, {0, 0}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View().Content
		if size[1] > 0 && len(strings.Split(view, "\n")) > size[1] {
			t.Fatalf("view exceeds height %d: %q", size[1], view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("view exceeds width %d: %q", size[0], line)
			}
		}
		if strings.Contains(view, "\x1b[38;") || strings.Contains(view, "\x1b[36m") {
			t.Fatal("NO_COLOR model rendered foreground colors")
		}
		if size[0] < 32 {
			if command := press(m, tea.KeyEnter); command != nil || m.page != review {
				t.Fatal("applied with an unreadable review")
			}
		}
	}
}

func TestLatePlansAndExternalChanges(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	command := press(m, tea.KeyEnter)
	press(m, tea.KeyEscape)
	m.Update(command())
	if m.page != configure || m.planning {
		t.Fatal("a cancelled review response changed navigation")
	}
	f.current.Hostname = "external-host"
	reviewPlan(t, m)
	if m.inputs[0].Value() != "external-host" || m.plan.Hostname.Changed {
		t.Fatal("unmodified field overwrote an external change")
	}
	press(m, tea.KeyEscape)
	edit(m, 0, "new-host")
	reviewPlan(t, m)
	f.current.Timezone = "Europe/London"
	press(m, tea.KeySpace)
	finishApplication(t, m, press(m, tea.KeyEnter))
	if m.err == nil || len(f.calls) != 0 {
		t.Fatal("stale plan changed the system")
	}
}

func TestNonTTY(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	if err := Run(context.Background(), f.engine(), strings.NewReader("yes\n"), io.Discard); err == nil || !strings.Contains(err.Error(), "interactive") || f.reads != 0 || len(f.calls) != 0 {
		t.Fatalf("non-TTY error=%v reads=%d calls=%v", err, f.reads, f.calls)
	}
}

func TestPasteAndLoadFailure(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	press(m, tea.KeyHome)
	press(m, 'k', tea.ModCtrl)
	m.Update(tea.PasteMsg{Content: "pasted-host"})
	if m.inputs[0].Value() != "pasted-host" || m.inputs[0].Err != nil || len(f.calls) != 0 {
		t.Fatal("bracketed paste did not use normal validation")
	}
	wantErr := errors.New("raw read diagnostic")
	engine := setup.NewEngine(func(context.Context) (setup.Configuration, error) {
		return setup.Configuration{}, wantErr
	}, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("load failure executed a command")
		return nil, nil
	})
	failed := newModel(context.Background(), engine, true)
	defer failed.cancel()
	initial := failed.Init()().(tea.BatchMsg)
	failed.Update(initial[0]())
	if !errors.Is(failed.err, wantErr) || strings.Contains(failed.View().Content, wantErr.Error()) {
		t.Fatal("load error was lost or exposed without requesting diagnostics")
	}
	press(failed, 'd')
	failed.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	if !strings.Contains(failed.View().Content, wantErr.Error()) {
		t.Fatal("load diagnostics are not accessible")
	}
}

func TestCancelBeforeCommandStarts(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	edit(m, 0, "new-host")
	reviewPlan(t, m)
	press(m, tea.KeySpace)
	command := press(m, tea.KeyEnter)
	press(m, 'c', tea.ModCtrl)
	finishApplication(t, m, command)
	if len(f.calls) != 0 || !errors.Is(m.err, context.Canceled) || !reflect.DeepEqual(m.result.Pending, []string{"hostname"}) {
		t.Fatalf("result=%+v error=%v calls=%v", m.result, m.err, f.calls)
	}
}

func TestHeadlessProgram(t *testing.T) {
	// Run Bubble Tea's actual event loop without a TTY, renderer, or native tools.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	m := newModel(ctx, f.engine(), true)
	defer m.cancel()
	observed := make(chan string, 3)
	program := tea.NewProgram(m, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(),
		tea.WithoutSignalHandler(), tea.WithWindowSize(80, 24), tea.WithContext(ctx),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			switch msg.(type) {
			case loadedMsg:
				observed <- "loaded"
			case plannedMsg:
				observed <- "planned"
			case appliedMsg:
				observed <- "applied"
			}
			return msg
		}))
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	await := func(want string) {
		t.Helper()
		select {
		case got := <-observed:
			if got != want {
				t.Fatalf("event=%q want=%q", got, want)
			}
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", want, ctx.Err())
		}
	}
	send := func(code rune) { program.Send(tea.KeyPressMsg(tea.Key{Code: code})) }
	await("loaded")
	send(tea.KeyEnter)
	send(tea.KeyEnter)
	await("planned")
	send(tea.KeySpace)
	send(tea.KeyEnter)
	await("applied")
	send(tea.KeyEnter)
	select {
	case err := <-done:
		if err != nil || m.err != nil || len(f.calls) != 0 || len(m.result.Unchanged) != 2 {
			t.Fatalf("program error=%v model error=%v result=%+v calls=%v", err, m.err, m.result, f.calls)
		}
	case <-ctx.Done():
		t.Fatal("program did not exit")
	}
}

func TestTerminalFailureJoinsApplication(t *testing.T) {
	f := &systemFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}, block: "timedatectl", entered: make(chan struct{})}
	m := readyModel(t, f)
	press(m, tea.KeyEnter)
	edit(m, 0, "new-host")
	edit(m, 1, "Europe/Kyiv")
	reviewPlan(t, m)
	press(m, tea.KeySpace)
	commands := press(m, tea.KeyEnter)().(tea.BatchMsg)
	go commands[0]()
	<-f.entered
	m.stop(io.ErrUnexpectedEOF)
	if !errors.Is(m.err, io.ErrUnexpectedEOF) || m.result.Failed != "timezone" || !reflect.DeepEqual(m.result.Applied, []string{"hostname"}) {
		t.Fatalf("terminal failure lost partial results: %+v, %v", m.result, m.err)
	}
	select {
	case <-m.application.done:
	default:
		t.Fatal("state-changing work outlived the terminal")
	}
}

func TestPipeIsNotTTY(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	f := &systemFixture{}
	if err := Run(context.Background(), f.engine(), read, write); err == nil || !strings.Contains(err.Error(), "interactive") || f.reads != 0 {
		t.Fatalf("pipe bypassed the TTY guard: %v", err)
	}
}
