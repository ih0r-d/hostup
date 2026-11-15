package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ih0r-d/hostup/src/internal/setup"
)

func TestWizardCommand(t *testing.T) {
	wantErr := errors.New("wizard result")
	engine := setup.NewEngine(nil, nil)
	ctx := context.WithValue(context.Background(), struct{}{}, "command-context")
	input, output := strings.NewReader(""), &bytes.Buffer{}
	called := false
	cmd := newSetupWizardCommand(engine, func(got context.Context, e *setup.Engine, in io.Reader, out io.Writer) error {
		called = true
		if got != ctx || e != engine || in != input || out != output {
			t.Fatal("wizard command lost its context, engine, or I/O")
		}
		return wantErr
	})
	cmd.SetIn(input)
	cmd.SetOut(output)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(nil)
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, wantErr) || !called {
		t.Fatalf("wizard error=%v called=%t", err, called)
	}
}

func TestRootWizardSafetyAndCompatibility(t *testing.T) {
	for _, args := range [][]string{
		{"setup", "wizard"}, {"setup", "wizard", "--yes"},
		{"setup", "wizard", "--hostname", "new-host"},
		{"setup", "wizard", "--timezone", "UTC"}, {"setup", "wizard", "extra"},
	} {
		root := NewRootCommand()
		root.SetIn(strings.NewReader(""))
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs(args)
		if err := root.Execute(); err == nil {
			t.Fatalf("unsafe wizard invocation accepted: %v", args)
		}
	}
	for _, args := range [][]string{{"version"}, {"info"}, {"doctor"}, {"service", "list"}, {"setup", "plan"}, {"setup", "apply"}, {"setup", "wizard"}} {
		cmd, _, err := NewRootCommand().Find(args)
		if err != nil || cmd.Name() != args[len(args)-1] {
			t.Fatalf("existing command not found: %v, %v", args, err)
		}
	}
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs(nil)
	if err := root.Execute(); err != nil || !strings.Contains(output.String(), "Usage:") {
		t.Fatalf("root without arguments changed behavior: output=%q error=%v", output.String(), err)
	}
}
