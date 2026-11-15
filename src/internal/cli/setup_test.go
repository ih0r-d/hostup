package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ih0r-d/hostup/src/internal/setup"
)

type setupFixture struct {
	current setup.Configuration
	reads   int
	calls   []string
	failAt  int
	err     error
}

func (f *setupFixture) engine() *setup.Engine {
	return setup.NewEngine(func(context.Context) (setup.Configuration, error) {
		f.reads++
		return f.current, nil
	}, func(_ context.Context, name string, args ...string) ([]byte, error) {
		f.calls = append(f.calls, name)
		if len(f.calls) == f.failAt {
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

func TestSetupPlanCommand(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		json bool
	}{
		{"human", []string{"plan", "--hostname", "new-host"}, false},
		{"JSON", []string{"plan", "--hostname", "new-host", "--json"}, true},
		{"parent flags", []string{"--hostname", "new-host", "plan", "--json"}, true},
		{"no changes", []string{"plan"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
			cmd := newSetupCommand(f.engine(), confirmService, func(io.Reader) bool { t.Fatal("plan checked terminal"); return false })
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil || len(f.calls) != 0 || f.reads != 1 {
				t.Fatalf("plan: error=%v reads=%d calls=%v", err, f.reads, f.calls)
			}
			if tt.json {
				var got setup.Plan
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				want := setup.Plan{
					Hostname: setup.Change{Current: "old-host", Desired: "new-host", Changed: true},
					Timezone: setup.Change{Current: "UTC", Desired: "UTC"},
				}
				if got != want {
					t.Fatalf("JSON plan=%+v; want %+v", got, want)
				}
			} else if !strings.Contains(out.String(), "timezone\tUTC\tUTC\tunchanged") || !strings.Contains(out.String(), "Hostup Setup Plan") {
				t.Fatalf("human plan=%q", out.String())
			}
		})
	}
}

func TestSetupConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name, input, want string
		terminal, yes     bool
		noChanges         bool
		promptErr         error
		prompts, calls    int
	}{
		{name: "accept", input: "yes\n", terminal: true, prompts: 1, calls: 1},
		{name: "decline", input: "n\n", terminal: true, prompts: 1, want: "cancelled"},
		{name: "EOF", terminal: true, prompts: 1, want: "EOF"},
		{name: "prompt failure", terminal: true, prompts: 1, promptErr: errors.New("prompt failed"), want: "prompt failed"},
		{name: "noninteractive", input: "yes\n", want: "--yes"},
		{name: "explicit yes", yes: true, calls: 1},
		{name: "no changes interactive", terminal: true, noChanges: true},
		{name: "no changes still requires yes in automation", noChanges: true, want: "--yes"},
		{name: "no changes explicit yes", noChanges: true, yes: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
			prompts := 0
			var out bytes.Buffer
			prompt := func(in io.Reader, writer io.Writer, question string) (bool, error) {
				prompts++
				if len(f.calls) != 0 || !strings.Contains(out.String(), "Hostup Setup Plan") {
					t.Fatal("confirmation must follow plan and precede execution")
				}
				if tt.promptErr != nil {
					return false, tt.promptErr
				}
				return confirmService(in, writer, question)
			}
			cmd := newSetupCommand(f.engine(), prompt, func(io.Reader) bool {
				if tt.yes {
					t.Fatal("--yes checked terminal")
				}
				return tt.terminal
			})
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetIn(strings.NewReader(tt.input))
			args := []string{"apply"}
			if !tt.noChanges {
				args = append(args, "--hostname", "new-host")
			}
			if tt.yes {
				args = append(args, "--yes")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			if (tt.want == "" && err != nil) || (tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want))) || prompts != tt.prompts || len(f.calls) != tt.calls {
				t.Fatalf("error=%v prompts=%d calls=%v output=%q", err, prompts, f.calls, out.String())
			}
		})
	}
}

func TestSetupBadArguments(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "--hostname="}, {"apply", "--timezone=", "--yes"},
		{"apply", "--hostname", "good", "--timezone", "Unknown/Zone", "--yes"},
		{"apply", "--hostname", "bad;id", "--yes"}, {"plan", "extra"},
		{"apply", "--json"}, {"plan", "--yes"}, {"plan", "--unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
			cmd := newSetupCommand(f.engine(), confirmService, setupIsTerminal)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil || f.reads != 0 || len(f.calls) != 0 {
				t.Fatalf("error=%v reads=%d calls=%v", err, f.reads, f.calls)
			}
		})
	}
}

func TestSetupApplyPartialFailures(t *testing.T) {
	wantErr := errors.New("operation failed")
	for _, tt := range []struct {
		name   string
		failAt int
		want   []string
	}{
		{"first failure", 1, []string{"Failed: hostname (may have changed", "Not attempted: timezone"}},
		{"partial failure", 2, []string{"Applied: hostname", "Failed: timezone (may have changed"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}, failAt: tt.failAt, err: wantErr}
			cmd := newSetupCommand(f.engine(), confirmService, setupIsTerminal)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"apply", "--hostname", "new-host", "--timezone", "Europe/Kyiv", "--yes"})
			if err := cmd.Execute(); !errors.Is(err, wantErr) || len(f.calls) != tt.failAt {
				t.Fatalf("error=%v calls=%v", err, f.calls)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output=%q missing %q", out.String(), want)
				}
			}
		})
	}
}

func TestSetupCancellation(t *testing.T) {
	for _, afterPrompt := range []bool{false, true} {
		f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
		ctx, cancel := context.WithCancel(context.Background())
		if !afterPrompt {
			cancel()
		}
		prompt := func(io.Reader, io.Writer, string) (bool, error) { cancel(); return true, nil }
		cmd := newSetupCommand(f.engine(), prompt, func(io.Reader) bool { return true })
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"apply", "--hostname", "new-host"})
		err := cmd.ExecuteContext(ctx)
		cancel()
		if !errors.Is(err, context.Canceled) || len(f.calls) != 0 {
			t.Fatalf("afterPrompt=%t error=%v calls=%v", afterPrompt, err, f.calls)
		}
	}
}

type setupOutput struct {
	allowed, writes int
	err             error
}

func (w *setupOutput) Write(data []byte) (int, error) {
	w.writes++
	if w.writes > w.allowed {
		return 0, w.err
	}
	return len(data), nil
}

func TestSetupOutputFailures(t *testing.T) {
	wantErr := errors.New("write failed")
	for _, tt := range []struct {
		args           []string
		allowed, calls int
	}{
		{[]string{"plan"}, 0, 0}, {[]string{"plan", "--json"}, 0, 0},
		{[]string{"apply", "--hostname", "new-host", "--yes"}, 0, 0},
		{[]string{"apply", "--hostname", "new-host", "--yes"}, 3, 1},
	} {
		f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
		cmd := newSetupCommand(f.engine(), confirmService, setupIsTerminal)
		cmd.SetOut(&setupOutput{allowed: tt.allowed, err: wantErr})
		cmd.SetErr(io.Discard)
		cmd.SetArgs(tt.args)
		if err := cmd.Execute(); !errors.Is(err, wantErr) || len(f.calls) != tt.calls {
			t.Fatalf("args=%v error=%v calls=%v", tt.args, err, f.calls)
		}
	}
}

func TestRootSetup(t *testing.T) {
	for _, sub := range []string{"plan", "apply"} {
		root := NewRootCommand()
		cmd, args, err := root.Find([]string{"setup", sub})
		if err != nil || cmd.Name() != sub || len(args) != 0 {
			t.Fatalf("Find(setup %s)=%v %v %v", sub, cmd, args, err)
		}
	}
	if runtime.GOOS != "linux" {
		for _, sub := range []string{"plan", "apply"} {
			root := NewRootCommand()
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs([]string{"setup", sub})
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unsupported on "+runtime.GOOS) {
				t.Fatalf("native setup %s error=%v", sub, err)
			}
		}
	}
	if setupIsTerminal(strings.NewReader("yes\n")) {
		t.Fatal("reader is not a terminal")
	}
}

func TestSetupCLIIdempotency(t *testing.T) {
	f := &setupFixture{current: setup.Configuration{Hostname: "old-host", Timezone: "UTC"}}
	for i := 0; i < 2; i++ {
		cmd := newSetupCommand(f.engine(), confirmService, setupIsTerminal)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"apply", "--hostname", "new-host", "--timezone", "Europe/Kyiv", "--yes"})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(f.calls, []string{"hostnamectl", "timedatectl"}) {
		t.Fatalf("repeated CLI execution calls=%v", f.calls)
	}
}

func TestSetupCLIVerificationFailure(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			current := setup.Configuration{Hostname: "old-host", Timezone: "UTC"}
			executed := false
			cause := errors.New("read failed")
			engine := setup.NewEngine(func(context.Context) (setup.Configuration, error) {
				if executed && unavailable {
					return setup.Configuration{}, cause
				}
				return current, nil
			}, func(context.Context, string, ...string) ([]byte, error) { executed = true; return nil, nil })
			cmd := newSetupCommand(engine, confirmService, func(io.Reader) bool { t.Fatal("--yes prompted"); return false })
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"apply", "--hostname", "new-host", "--yes"})
			err := cmd.Execute()
			want := setup.ErrVerificationMismatch
			if unavailable {
				want = setup.ErrVerificationUnavailable
			}
			if !errors.Is(err, want) || !strings.Contains(out.String(), "Executed (not verified): hostname") || strings.Contains(out.String(), "Applied: hostname") {
				t.Fatalf("error %v output %s", err, out.String())
			}
		})
	}
}
