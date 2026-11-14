package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPlatforms(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows", "freebsd"} {
		if err := requireLinux(goos); (err == nil) != (goos == "linux") {
			t.Fatalf("platform %s: %v", goos, err)
		}
	}
	if runtime.GOOS != "linux" {
		e := NewEngine(nil, nil)
		if _, err := e.Plan(context.Background(), Desired{}); err == nil || !strings.Contains(err.Error(), "unsupported on "+runtime.GOOS) {
			t.Fatalf("native Plan: %v", err)
		}
		if _, err := e.Apply(context.Background(), Plan{}); err == nil || !strings.Contains(err.Error(), "unsupported on "+runtime.GOOS) {
			t.Fatalf("native Apply: %v", err)
		}
	}
}

func TestReadTimezone(t *testing.T) {
	for _, tt := range []struct {
		name, target, output string
		regular, fail        bool
		want                 string
		calls                int
	}{
		{name: "absolute symlink", target: "Europe/Kyiv", want: "Europe/Kyiv"},
		{name: "relative symlink", target: "UTC", want: "UTC"},
		{name: "dangling symlink", target: "UTC", output: "Europe/Kyiv\n", want: "Europe/Kyiv", calls: 1},
		{name: "copied localtime", regular: true, output: "UTC\n", want: "UTC", calls: 1},
		{name: "missing localtime", output: "Europe/Kyiv\n", want: "Europe/Kyiv", calls: 1},
		{name: "outside zoneinfo", target: "../other/UTC", output: "UTC\n", want: "UTC", calls: 1},
		{name: "unknown link", target: "Unknown/Zone", output: "UTC\n", want: "UTC", calls: 1},
		{name: "fallback failure", regular: true, fail: true, calls: 1},
		{name: "empty timezone", regular: true, calls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			zoneDir := filepath.Join(root, "zoneinfo")
			localtime := filepath.Join(root, "localtime")
			if tt.target != "" {
				target := filepath.Join(zoneDir, tt.target)
				if tt.name != "dangling symlink" {
					if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				if tt.name == "relative symlink" {
					target = filepath.Join("zoneinfo", tt.target)
				}
				if err := os.Symlink(target, localtime); err != nil {
					t.Fatal(err)
				}
			} else if tt.regular {
				if err := os.WriteFile(localtime, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			run := func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if name != "timedatectl" || !reflect.DeepEqual(args, []string{"--no-ask-password", "show", "--property=Timezone", "--value"}) {
					t.Fatalf("unexpected read command: %s %v", name, args)
				}
				if tt.fail {
					return nil, errors.New("no system bus")
				}
				return []byte(tt.output), nil
			}
			got, err := readTimezone(context.Background(), localtime, zoneDir, run)
			if got != tt.want || calls != tt.calls || (err == nil) != (tt.want != "") {
				t.Fatalf("timezone=%q err=%v calls=%d", got, err, calls)
			}
		})
	}
}

func TestCommandErrors(t *testing.T) {
	for _, tt := range []struct {
		name, stderr, want string
		err                error
	}{
		{name: "permission", err: os.ErrPermission, want: "insufficient permissions"},
		{name: "missing executable", err: exec.ErrNotFound, want: "is unavailable"},
		{name: "polkit", stderr: "Interactive authentication required.", want: "insufficient permissions"},
		{name: "access denied", stderr: "Access denied", want: "insufficient permissions"},
		{name: "systemd unavailable", stderr: "System has not been booted with systemd", want: "not been booted"},
		{name: "other", err: errors.New("execution error"), want: "execution error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := tt.err
			if tt.stderr != "" {
				original = &exec.ExitError{Stderr: []byte(tt.stderr)}
			}
			e := NewEngine(nil, func(context.Context, string, ...string) ([]byte, error) { return nil, original })
			if _, err := e.command(context.Background(), "hostnamectl"); err == nil || !strings.Contains(err.Error(), tt.want) || !errors.Is(err, original) {
				t.Fatalf("command error=%v; want %q wrapping %v", err, tt.want, original)
			}
		})
	}
}

// The only real subprocess used by setup tests is this test binary, never a
// system administration command. This also verifies cancellation without a shell.
func TestSetupCommandHelper(t *testing.T) {
	if os.Getenv("HOSTUP_SETUP_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	if mode == "wait" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	fmt.Fprint(os.Stdout, mode)
	os.Exit(0)
}

func TestNativeExecution(t *testing.T) {
	t.Setenv("HOSTUP_SETUP_HELPER", "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"literal;$(touch should-never-exist)", "wait"} {
		t.Run(mode, func(t *testing.T) {
			e := NewEngine(nil, func(ctx context.Context, _ string, args ...string) ([]byte, error) {
				return runCommand(ctx, executable, "-test.run=^TestSetupCommandHelper$", "--", args[0])
			})
			if mode == "wait" {
				e.timeout = 20 * time.Millisecond
			}
			out, err := e.command(context.Background(), "test-helper", mode)
			if mode == "wait" {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("timeout error=%v", err)
				}
			} else if err != nil || string(out) != mode {
				t.Fatalf("output=%q err=%v", out, err)
			}
		})
	}
}
