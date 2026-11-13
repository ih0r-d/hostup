package system

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

type fakeServiceExecutor struct {
	out        []byte
	err        error
	name       string
	args       []string
	streamOut  string
	streamErr  error
	streamWait bool
	started    chan struct{}
}

func (f *fakeServiceExecutor) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.name, f.args = name, append([]string(nil), args...)
	return f.out, f.err
}
func (f *fakeServiceExecutor) Stream(ctx context.Context, dst io.Writer, name string, args ...string) error {
	f.name, f.args = name, append([]string(nil), args...)
	_, _ = io.WriteString(dst, f.streamOut)
	if f.started != nil {
		close(f.started)
	}
	if f.streamWait {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.streamErr
}

func TestValidateServiceName(t *testing.T) {
	for _, tt := range []struct {
		name  string
		valid bool
	}{
		{"ssh", true}, {"ssh.service", true}, {"my-service@instance_1", true}, {"a.b-c_d@x.service", true},
		{"", false}, {"-bad", false}, {"../etc/passwd", false}, {"foo..bar", false}, {"foo;touch /tmp/x", false}, {"foo$(id)", false}, {"foo`id`", false}, {"foo bar", false}, {"foo/service", false}, {"foo.service.service", false}, {strings.Repeat("a", 256), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceName(tt.name)
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateServiceName(%q) error = %v", tt.name, err)
			}
		})
	}
}

func TestServiceManagerOperations(t *testing.T) {
	for _, tt := range []struct{ action string }{{"start"}, {"stop"}, {"restart"}, {"enable"}, {"disable"}} {
		t.Run(tt.action, func(t *testing.T) {
			fake := &fakeServiceExecutor{}
			if err := NewServiceManager(fake).Change(context.Background(), tt.action, "sshd"); err != nil {
				t.Fatal(err)
			}
			if fake.name != "systemctl" || !reflect.DeepEqual(fake.args, []string{tt.action, "sshd.service"}) {
				t.Fatalf("command = %s %v", fake.name, fake.args)
			}
		})
	}
	for _, tt := range []struct {
		name, action string
		errText      string
	}{{"bad;id", "start", "invalid service name"}, {"sshd", "reload", "unsupported service action"}} {
		fake := &fakeServiceExecutor{}
		err := NewServiceManager(fake).Change(context.Background(), tt.action, tt.name)
		if err == nil || !strings.Contains(err.Error(), tt.errText) || fake.name != "" {
			t.Fatalf("Change(%q,%q) = %v, command %q", tt.action, tt.name, err, fake.name)
		}
	}
}

func TestServiceListAndStatus(t *testing.T) {
	fake := &fakeServiceExecutor{out: []byte("ssh.service loaded active running OpenBSD Secure Shell server\ncron.service loaded inactive dead Regular background program processing daemon\ninvalid row\n")}
	m := NewServiceManager(fake)
	got, err := m.List(context.Background())
	want := []Service{{"ssh.service", "loaded", "active", "running"}, {"cron.service", "loaded", "inactive", "dead"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %#v, %v", got, err)
	}
	if fake.name != "systemctl" || !reflect.DeepEqual(fake.args, []string{"list-units", "--type=service", "--all", "--no-legend", "--no-pager"}) {
		t.Fatalf("List command = %s %v", fake.name, fake.args)
	}
	fake.out = []byte("Unit ssh.service could not be found.\n")
	status, err := m.Status(context.Background(), "ssh")
	if err != nil || string(status) != string(fake.out) {
		t.Fatalf("Status() = %q, %v", status, err)
	}
	if !reflect.DeepEqual(fake.args, []string{"status", "--no-pager", "ssh.service"}) {
		t.Fatalf("Status args = %v", fake.args)
	}
}

func TestServiceCommandErrors(t *testing.T) {
	for _, tt := range []struct{ stderr, want string }{
		{"Unit missing.service could not be found.", "service not found"},
		{"Authentication is required", "insufficient permissions"},
		{"Failed to connect to bus", "systemd is unavailable"},
		{"other failure", "other failure"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			fake := &fakeServiceExecutor{err: &exec.ExitError{Stderr: []byte(tt.stderr)}}
			_, err := NewServiceManager(fake).Status(context.Background(), "ssh")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Status error = %v, want %q", err, tt.want)
			}
		})
	}
	fake := &fakeServiceExecutor{err: exec.ErrNotFound}
	if err := NewServiceManager(fake).Change(context.Background(), "start", "ssh"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("missing binary error = %v", err)
	}
}

func TestServiceLogsStreamingAndCancellation(t *testing.T) {
	fake := &fakeServiceExecutor{streamOut: "first\nsecond\n"}
	var output bytes.Buffer
	if err := NewServiceManager(fake).Logs(context.Background(), &output, "ssh", 25, false); err != nil {
		t.Fatal(err)
	}
	if output.String() != fake.streamOut || fake.name != "journalctl" || !reflect.DeepEqual(fake.args, []string{"--no-pager", "--unit", "ssh.service", "--lines", "25"}) {
		t.Fatalf("logs output %q command %s %v", output.String(), fake.name, fake.args)
	}
	ctx, cancel := context.WithCancel(context.Background())
	fake.streamWait, fake.streamOut, fake.started = true, "streamed", make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- NewServiceManager(fake).Logs(ctx, &output, "ssh.service", 5, true) }()
	<-fake.started
	if !strings.Contains(output.String(), "streamed") {
		t.Fatal("stream was not written before cancellation")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("follow error = %v", err)
	}
	if !reflect.DeepEqual(fake.args, []string{"--no-pager", "--unit", "ssh.service", "--lines", "5", "--follow"}) {
		t.Fatalf("follow args = %v", fake.args)
	}
}

func TestServiceLogsArgumentValidation(t *testing.T) {
	for _, tt := range []struct {
		name  string
		lines int
	}{{"bad;id", 5}, {"ssh", 0}, {"ssh", 10001}} {
		fake := &fakeServiceExecutor{}
		err := NewServiceManager(fake).Logs(context.Background(), io.Discard, tt.name, tt.lines, false)
		if err == nil || fake.name != "" {
			t.Fatalf("Logs(%q,%d) = %v, command %q", tt.name, tt.lines, err, fake.name)
		}
	}
	fake := &fakeServiceExecutor{streamErr: errors.New("journal failure")}
	if err := NewServiceManager(fake).Logs(context.Background(), io.Discard, "ssh", 10, false); err == nil || !strings.Contains(err.Error(), "journal failure") {
		t.Fatalf("Logs error = %v", err)
	}
}
