package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/spf13/cobra"
)

func TestNativeCompletionGeneration(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			cmd := NewRootCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"completion", shell})
			if err := cmd.Execute(); err != nil || !strings.Contains(out.String(), "__complete") {
				t.Fatalf("generation error=%v output=%q", err, out.String())
			}
			if shell == "bash" || shell == "zsh" {
				binary, err := exec.LookPath(shell)
				if err != nil {
					t.Skipf("%s unavailable for syntax check", shell)
				}
				check := exec.Command(binary, "-n")
				check.Stdin = bytes.NewReader(out.Bytes())
				if result, err := check.CombinedOutput(); err != nil {
					t.Fatalf("syntax: %v, %s", err, result)
				}
			}
		})
	}
}

type completionExecutor struct {
	calls                 []string
	loaded, files         string
	failLoaded, failFiles bool
	block                 bool
}

func (f *completionExecutor) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args[0])
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	switch args[0] {
	case "list-units":
		if f.failLoaded {
			return nil, os.ErrNotExist
		}
		return []byte(f.loaded), nil
	case "list-unit-files":
		if f.failFiles {
			return nil, os.ErrNotExist
		}
		return []byte(f.files), nil
	default:
		return nil, errors.New("completion attempted a non-read-only operation")
	}
}

func (*completionExecutor) Stream(context.Context, io.Writer, string, ...string) error {
	return errors.New("completion must not stream logs or start a TUI")
}

func TestServiceCompletion(t *testing.T) {
	for _, tt := range []struct {
		name, prefix                     string
		args                             []string
		failLoaded, failFiles, cancelled bool
		want                             []string
	}{
		{name: "all", want: []string{"cron.service", "disabled.service", "ssh.service", "transient.service"}},
		{name: "prefix", prefix: "ssh", want: []string{"ssh.service"}},
		{name: "installed only", failLoaded: true, want: []string{"disabled.service", "ssh.service"}},
		{name: "loaded only", failFiles: true, want: []string{"cron.service", "ssh.service", "transient.service"}},
		{name: "unavailable", failLoaded: true, failFiles: true},
		{name: "second argument", args: []string{"ssh.service"}},
		{name: "cancelled", cancelled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &completionExecutor{loaded: "ssh.service loaded active running SSH\ncron.service loaded inactive dead cron\ntransient.service loaded active running transient\n", files: "ssh.service enabled enabled\ndisabled.service disabled enabled\nbad;name.service enabled enabled\nother.socket enabled enabled\n", failLoaded: tt.failLoaded, failFiles: tt.failFiles}
			cmd := &cobra.Command{Use: "status"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			cmd.SetContext(ctx)
			got, directive := completeServices(system.NewServiceManager(f))(cmd, tt.args, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) || directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf("completion=%v directive=%v want=%v", got, directive, tt.want)
			}
			if (tt.cancelled || len(tt.args) > 0) && len(f.calls) != 0 {
				t.Fatal("unnecessary query")
			}
		})
	}
}

func TestCompletionDeadline(t *testing.T) {
	f := &completionExecutor{block: true}
	cmd := &cobra.Command{Use: "status"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	cmd.SetContext(ctx)
	got, directive := completeServices(system.NewServiceManager(f))(cmd, nil, "")
	if len(got) != 0 || directive != cobra.ShellCompDirectiveNoFileComp || ctx.Err() == nil {
		t.Fatalf("completion=%v directive=%v", got, directive)
	}
}

func TestTimezoneCompletion(t *testing.T) {
	for _, tt := range []struct {
		name, command, prefix              string
		missingTables, missingTool, cancel bool
		want                               []string
	}{
		{name: "table", want: []string{"Europe/Kyiv", "Europe/London", "UTC"}},
		{name: "prefix", prefix: "Europe/K", want: []string{"Europe/Kyiv"}},
		{name: "command fallback", missingTables: true, want: []string{"Etc/UTC", "UTC"}},
		{name: "offline", missingTables: true, missingTool: true, want: []string{"UTC"}},
		{name: "offline unmatched", prefix: "Europe/", missingTables: true, missingTool: true},
		{name: "wizard does not use flags", command: "wizard"},
		{name: "cancelled", cancel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reads, calls := 0, 0
			fn := completeTimezones(func(string) ([]byte, error) {
				reads++
				if tt.missingTables {
					return nil, os.ErrNotExist
				}
				return []byte("# country coordinates timezone\nUA +5026+03031 Europe/Kyiv\nGB +513030-0000731 Europe/London\nUA +5026+03031 Europe/Kyiv\n"), nil
			}, func(ctx context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("query without deadline")
				}
				if name != "timedatectl" || !reflect.DeepEqual(args, []string{"--no-ask-password", "list-timezones", "--no-pager"}) {
					t.Fatal("not a read-only timezone query")
				}
				if tt.missingTool {
					return nil, exec.ErrNotFound
				}
				return []byte("Etc/UTC\nUTC\n"), nil
			})
			cmd := &cobra.Command{Use: tt.command}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			cmd.SetContext(ctx)
			got, directive := fn(cmd, nil, tt.prefix)
			if !reflect.DeepEqual(got, tt.want) || directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf("got=%v directive=%v want=%v", got, directive, tt.want)
			}
			if (tt.command == "wizard" || tt.cancel) && (reads != 0 || calls != 0) {
				t.Fatal("unnecessary query")
			}
			if !tt.missingTables && calls != 0 {
				t.Fatal("local tables triggered a command")
			}
		})
	}
}

func TestCompletionProtocolAndRegistration(t *testing.T) {
	for _, args := range [][]string{{"__complete", "ser"}, {"__complete", "service", "st"}, {"__complete", "setup", "plan", "--time"}} {
		root := NewRootCommand()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(io.Discard)
		root.SetArgs(args)
		if err := root.Execute(); err != nil || !strings.Contains(out.String(), ":") {
			t.Fatalf("protocol=%q error=%v", out.String(), err)
		}
	}
	f := &completionExecutor{loaded: "ssh.service loaded active running SSH\n"}
	root := &cobra.Command{Use: "hostup"}
	service := newServiceCommand(system.NewServiceManager(f), func(io.Reader, io.Writer, string) (bool, error) { t.Fatal("completion prompted"); return false, nil })
	root.AddCommand(service)
	for _, command := range service.Commands() {
		if command.Name() != "list" && command.ValidArgsFunction == nil {
			t.Fatalf("%s lacks unit completion", command.Name())
		}
	}
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"__complete", "service", "start", "ssh"})
	if err := root.Execute(); err != nil || !strings.Contains(out.String(), "ssh.service") {
		t.Fatalf("completion=%q error=%v", out.String(), err)
	}
	for _, call := range f.calls {
		if call != "list-units" && call != "list-unit-files" {
			t.Fatal("completion mutated systemd")
		}
	}
	setupCmd, _, _ := NewRootCommand().Find([]string{"setup"})
	for _, flag := range []string{"hostname", "timezone"} {
		if _, ok := setupCmd.GetFlagCompletionFunc(flag); !ok {
			t.Fatalf("missing %s callback", flag)
		}
	}
}
