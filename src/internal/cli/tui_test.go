package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestTUIRouting(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		args                   []string
		tty, launch, wantError bool
	}{
		{name: "interactive root", tty: true, launch: true},
		{name: "redirected root"},
		{name: "explicit", args: []string{"tui"}, tty: true, launch: true},
		{name: "non-TTY explicit", args: []string{"tui"}, wantError: true},
		{name: "extra argument", args: []string{"tui", "extra"}, tty: true, wantError: true},
		{name: "version", args: []string{"version"}, tty: true},
		{name: "version flag", args: []string{"--version"}, tty: true},
		{name: "help", args: []string{"--help"}, tty: true},
		{name: "service help", args: []string{"service", "--help"}, tty: true},
		{name: "setup help", args: []string{"setup", "--help"}, tty: true},
		{name: "completion", args: []string{"completion", "bash"}, tty: true},
		{name: "dynamic completion", args: []string{"__complete", "tu"}, tty: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input, output := strings.NewReader(""), &bytes.Buffer{}
			ctx := context.WithValue(context.Background(), struct{}{}, "caller")
			calls := 0
			root := newRootCommand(func(got context.Context, in io.Reader, out io.Writer) error {
				calls++
				if got != ctx || in != input || out != output {
					t.Fatal("TUI lost caller context or streams")
				}
				return nil
			}, func(in io.Reader, out io.Writer) bool { return tt.tty && in == input && out == output })
			root.SetIn(input)
			root.SetOut(output)
			root.SetErr(io.Discard)
			root.SetArgs(tt.args)
			err := root.ExecuteContext(ctx)
			if (err != nil) != tt.wantError || (calls == 1) != tt.launch {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if len(tt.args) == 0 && !tt.tty && (!strings.Contains(output.String(), "Usage: hostup") || strings.Count(output.String(), "\n") > 6) {
				t.Fatalf("noninteractive usage: %q", output.String())
			}
			if tt.name == "non-TTY explicit" && !strings.Contains(err.Error(), "interactive input and output") {
				t.Fatal(err)
			}
			if tt.name == "dynamic completion" && !strings.Contains(output.String(), "tui") {
				t.Fatal("tui is not completed")
			}
		})
	}
}

func TestTUIRunFailureAndRealNonTTY(t *testing.T) {
	want := errors.New("renderer failed")
	root := newRootCommand(func(context.Context, io.Reader, io.Writer) error { return want }, func(io.Reader, io.Writer) bool { return true })
	root.SetArgs([]string{"tui"})
	if err := root.Execute(); !errors.Is(err, want) {
		t.Fatalf("lost TUI error: %v", err)
	}
	root = NewRootCommand()
	root.SetIn(strings.NewReader(""))
	root.SetOut(io.Discard)
	root.SetArgs([]string{"tui"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "interactive") {
		t.Fatalf("non-TTY: %v", err)
	}
}
