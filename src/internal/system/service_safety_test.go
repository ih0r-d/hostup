package system

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

type waitingServiceExecutor struct{}

func (waitingServiceExecutor) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (waitingServiceExecutor) Stream(ctx context.Context, _ io.Writer, _ string, _ ...string) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestServiceFiniteOperationsHaveDeadlines(t *testing.T) {
	for _, operation := range []string{"change", "logs"} {
		t.Run(operation, func(t *testing.T) {
			manager := NewServiceManager(waitingServiceExecutor{})
			manager.timeout = time.Millisecond
			var err error
			if operation == "change" {
				err = manager.Change(context.Background(), "start", "fixture")
			} else {
				err = manager.Logs(context.Background(), io.Discard, "fixture", 10, false)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
	fake := &fakeServiceExecutor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewServiceManager(fake).Change(ctx, "stop", "fixture"); !errors.Is(err, context.Canceled) || fake.name != "" {
		t.Fatalf("cancelled change: %v %s", err, fake.name)
	}
}

func TestServiceErrorsPreserveCauses(t *testing.T) {
	for _, stderr := range []string{"Unit not found", "Access denied", "Interactive authentication required.", "Failed to connect to bus", "other native error"} {
		original := &exec.ExitError{Stderr: []byte(stderr)}
		err := commandError("inspect", "systemctl", original)
		var exit *exec.ExitError
		if !errors.Is(err, original) || !errors.As(err, &exit) {
			t.Fatal("lost native error cause", err)
		}
		if (stderr == "Access denied" || stderr == "Interactive authentication required.") && !errors.Is(err, os.ErrPermission) {
			t.Fatal("unclassifiable permission error")
		}
	}
	if err := commandError("inspect", "systemctl", exec.ErrNotFound); !errors.Is(err, exec.ErrNotFound) {
		t.Fatal("lost missing executable", err)
	}
}

func TestInactiveServiceDoesNotSuppressCancellation(t *testing.T) {
	exit := exec.Command("sh", "-c", "exit 3").Run()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		fake := &fakeServiceExecutor{out: []byte("fixture.service\nActive: inactive\n"), err: errors.Join(exit, cause)}
		if _, err := NewServiceManager(fake).Status(context.Background(), "fixture"); !errors.Is(err, cause) {
			t.Fatal("cancellation converted to status success", err)
		}
	}
}
