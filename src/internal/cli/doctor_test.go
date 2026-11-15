package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ih0r-d/hostup/src/internal/system"
)

func TestDoctorCommand(t *testing.T) {
	report := system.Diagnostics{
		Platform: system.Platform{OS: "linux", Distribution: "debian", Version: "12", Architecture: "arm64", Hostname: "pi"},
		Memory:   system.MemoryInfo{TotalBytes: 4 << 30, AvailableBytes: 2 << 30},
		Disk:     system.DiskInfo{Path: "/", TotalBytes: 32 << 30, UsedBytes: 8 << 30, AvailableBytes: 23 << 30},
		CPU:      system.CPUInfo{LogicalCPUs: 4},
	}
	diagnosticErr := errors.New("diagnostic failed")
	for _, tt := range []struct {
		name    string
		args    []string
		err     error
		wantErr string
	}{
		{name: "human"},
		{name: "JSON", args: []string{"--json"}},
		{name: "human failure", err: diagnosticErr, wantErr: "diagnostic failed"},
		{name: "JSON failure", args: []string{"--json"}, err: diagnosticErr, wantErr: "diagnostic failed"},
		{name: "reject positional arguments", args: []string{"extra"}, wantErr: "unknown command"},
		{name: "reject unknown flag", args: []string{"--missing"}, wantErr: "unknown flag"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			cmd := newDoctorCommand(func() (system.Diagnostics, error) {
				called = true
				return report, tt.err
			})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				if tt.err != nil && !errors.Is(err, tt.err) {
					t.Errorf("error = %v, want wrapping %v", err, tt.err)
				}
				if called != (tt.err != nil) || out.Len() != 0 {
					t.Errorf("diagnose called = %t, output = %q", called, out.String())
				}
				return
			}
			if err != nil || !called {
				t.Fatalf("error = %v, diagnose called = %t", err, called)
			}
			if tt.name == "JSON" {
				var got system.Diagnostics
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got != report {
					t.Errorf("JSON report = %+v, want %+v", got, report)
				}
				var keys map[string]json.RawMessage
				if err := json.Unmarshal(out.Bytes(), &keys); err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"platform", "memory", "disk", "cpu"} {
					if _, ok := keys[key]; !ok {
						t.Errorf("JSON missing key %q", key)
					}
				}
				return
			}
			want := "Hostup Diagnostics\nOS:           linux (debian 12)\nArchitecture: arm64\nHostname:     pi\nCPU:          4 logical CPUs\nMemory:       2.00 GiB available / 4.00 GiB total\nDisk (/):     8.00 GiB used / 32.00 GiB total (23.00 GiB available)\n"
			if out.String() != want {
				t.Errorf("output = %q, want %q", out.String(), want)
			}
		})
	}
}

func TestDoctorOutputError(t *testing.T) {
	wantErr := errors.New("write failed")
	for _, args := range [][]string{nil, {"--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newDoctorCommand(func() (system.Diagnostics, error) { return system.Diagnostics{}, nil })
			cmd.SetOut(failingWriter{wantErr})
			cmd.SetErr(io.Discard)
			cmd.SilenceUsage, cmd.SilenceErrors = true, true
			cmd.SetArgs(args)
			if err := cmd.Execute(); !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want wrapping %v", err, wantErr)
			}
		})
	}
}

func TestRootDoctor(t *testing.T) {
	root := NewRootCommand()
	cmd, args, err := root.Find([]string{"doctor", "--json"})
	if err != nil || cmd.Name() != "doctor" || !reflect.DeepEqual(args, []string{"--json"}) {
		t.Fatalf("Find(doctor) = %v, %v, %v", cmd, args, err)
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Fatal("doctor missing --json")
	}
	if runtime.GOOS != "linux" {
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"doctor", "--json"})
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unsupported OS: "+runtime.GOOS) {
			t.Fatalf("root doctor error = %v", err)
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
