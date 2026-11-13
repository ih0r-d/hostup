package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ih0r-d/hostup/internal/system"
)

type cliServiceExecutor struct {
	output  []byte
	err     error
	command string
	args    []string
}

func (f *cliServiceExecutor) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.command = name
	f.args = append([]string(nil), args...)
	return f.output, f.err
}
func (f *cliServiceExecutor) Stream(_ context.Context, w io.Writer, name string, args ...string) error {
	f.command = name
	f.args = append([]string(nil), args...)
	_, err := w.Write([]byte("log line\n"))
	return err
}

func executeService(t *testing.T, fake *cliServiceExecutor, input string, args ...string) (string, error) {
	t.Helper()
	root := newServiceCommand(system.NewServiceManager(fake), confirmService)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetIn(strings.NewReader(input))
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.SetArgs(args)
	serviceIsTerminal = func(io.Reader) bool { return input != "" }
	defer func() { serviceIsTerminal = isTerminal }()
	err := root.Execute()
	return out.String(), err
}

func TestServiceCLIReadOnlyJSON(t *testing.T) {
	for _, tt := range []struct {
		args   []string
		output []byte
	}{
		{[]string{"list", "--json"}, []byte("sshd.service loaded active running daemon\n")},
		{[]string{"status", "sshd", "--json"}, []byte("status text")},
	} {
		fake := &cliServiceExecutor{output: tt.output}
		out, err := executeService(t, fake, "", tt.args...)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err = json.Unmarshal([]byte(out), &decoded); err != nil {
			t.Fatalf("invalid JSON %q: %v", out, err)
		}
		if fake.command != "systemctl" {
			t.Fatalf("command %q", fake.command)
		}
	}
}

func TestServiceCLIChanges(t *testing.T) {
	for _, action := range []string{"start", "stop", "restart", "enable", "disable"} {
		t.Run(action, func(t *testing.T) {
			fake := &cliServiceExecutor{}
			out, err := executeService(t, fake, "yes\n", action, "sshd")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, "? ") || fake.command != "systemctl" || !reflect.DeepEqual(fake.args, []string{action, "sshd.service"}) {
				t.Fatalf("out %q command %s %v", out, fake.command, fake.args)
			}
		})
	}
	for _, tc := range []struct {
		name, input, want string
		yes               bool
	}{
		{"declined", "n\n", "cancelled", false}, {"noninteractive", "", "interactive terminal", false}, {"yes", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &cliServiceExecutor{}
			args := []string{"start", "sshd"}
			if tc.yes {
				args = append(args, "--yes")
			}
			_, err := executeService(t, fake, tc.input, args...)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || fake.command != "" {
				t.Fatalf("error %v command %q", err, fake.command)
			}
		})
	}
	fake := &cliServiceExecutor{err: errors.New("denied")}
	_, err := executeService(t, fake, "", "start", "sshd", "--yes")
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("command error %v", err)
	}
	_, err = executeService(t, &cliServiceExecutor{}, "yes\n", "start", "bad;id")
	if err == nil || !strings.Contains(err.Error(), "invalid service name") {
		t.Fatalf("invalid name error %v", err)
	}
}
