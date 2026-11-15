package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/ih0r-d/hostup/src/internal/system"
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

func TestServiceCLIList(t *testing.T) {
	for _, tt := range []struct {
		name, output, human string
		want                []system.Service
	}{
		{
			name:   "all states",
			output: "ssh.service loaded active running daemon\ncron.service loaded inactive dead daemon\nbroken.service loaded failed failed daemon\n",
			human:  "UNIT\tLOAD\tACTIVE\tSUB\nssh.service\tloaded\tactive\trunning\ncron.service\tloaded\tinactive\tdead\nbroken.service\tloaded\tfailed\tfailed\n",
			want:   []system.Service{{Name: "ssh.service", Load: "loaded", Active: "active", Sub: "running"}, {Name: "cron.service", Load: "loaded", Active: "inactive", Sub: "dead"}, {Name: "broken.service", Load: "loaded", Active: "failed", Sub: "failed"}},
		},
		{name: "empty", human: "No services found\n", want: []system.Service{}},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", tt.name, jsonOutput), func(t *testing.T) {
				args := []string{"list"}
				if jsonOutput {
					args = append(args, "--json")
				}
				fake := &cliServiceExecutor{output: []byte(tt.output)}
				out, err := executeService(t, fake, "", args...)
				if err != nil {
					t.Fatal(err)
				}
				if jsonOutput {
					var got []system.Service
					if err := json.Unmarshal([]byte(out), &got); err != nil || !reflect.DeepEqual(got, tt.want) {
						t.Fatalf("list JSON = %q, %v; want %+v", out, err, tt.want)
					}
					if len(tt.want) == 0 && out != "[]\n" {
						t.Fatalf("empty list JSON = %q", out)
					}
				} else if out != tt.human {
					t.Fatalf("list output = %q; want %q", out, tt.human)
				}
			})
		}
	}
}

func TestServiceCLIStatus(t *testing.T) {
	exitErr := exec.Command("sh", "-c", "exit 3").Run()
	var statusErr *exec.ExitError
	if !errors.As(exitErr, &statusErr) {
		t.Fatalf("create inactive status error: %v", exitErr)
	}
	for _, state := range []struct{ active, sub string }{{"active", "running"}, {"inactive", "dead"}, {"failed", "failed"}} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", state.active, jsonOutput), func(t *testing.T) {
				args := []string{"status", "ssh"}
				human := fmt.Sprintf("ssh.service\nActive: %s (%s)\n", state.active, state.sub)
				fake := &cliServiceExecutor{output: []byte(human)}
				if state.active != "active" {
					fake.err = statusErr
				}
				if jsonOutput {
					args = append(args, "--json")
					fake.output = []byte(fmt.Sprintf("Id=ssh.service\nLoadState=loaded\nActiveState=%s\nSubState=%s\n", state.active, state.sub))
					fake.err = nil // systemctl show succeeds for inactive and failed units.
				}
				out, err := executeService(t, fake, "", args...)
				if err != nil {
					t.Fatal(err)
				}
				if jsonOutput {
					var got map[string]string
					want := map[string]string{"name": "ssh.service", "load": "loaded", "active": state.active, "sub": state.sub}
					if err := json.Unmarshal([]byte(out), &got); err != nil || !reflect.DeepEqual(got, want) {
						t.Fatalf("status JSON = %q, %v; want %v", out, err, want)
					}
				} else if out != human {
					t.Fatalf("status output = %q; want %q", out, human)
				}
			})
		}
	}
}

func TestServiceCLIReadFailures(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"list", "--json"}, {"status", "ssh"}, {"status", "ssh", "--json"}} {
		for _, tt := range []struct {
			name, want string
			err        error
		}{
			{name: "execution", err: errors.New("execution failed"), want: "execution failed"},
			{name: "permission", err: &exec.ExitError{Stderr: []byte("Access denied")}, want: "insufficient permissions"},
			{name: "missing", err: &exec.ExitError{Stderr: []byte("Unit ssh.service could not be found.")}, want: "service not found"},
		} {
			t.Run(strings.Join(args, " ")+"/"+tt.name, func(t *testing.T) {
				fake := &cliServiceExecutor{output: []byte("partial output\n"), err: tt.err}
				out, err := executeService(t, fake, "", args...)
				if err == nil || !strings.Contains(err.Error(), tt.want) || out != "" {
					t.Fatalf("output = %q, error = %v; want error %q and no success output", out, err, tt.want)
				}
			})
		}
	}
	fake := &cliServiceExecutor{output: []byte("Id=ssh.service\nLoadState=not-found\nActiveState=inactive\nSubState=dead\n")}
	out, err := executeService(t, fake, "", "status", "ssh", "--json")
	if err == nil || !strings.Contains(err.Error(), "service not found") || out != "" {
		t.Fatalf("missing JSON status: output %q, error %v", out, err)
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
			if !strings.Contains(out, "? ") || fake.command != "systemctl" || !reflect.DeepEqual(fake.args, []string{action, "--no-ask-password", "sshd.service"}) {
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
